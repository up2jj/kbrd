package script

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	defaultHTTPTimeout          = 10 * time.Second
	defaultHTTPMaxResponseBytes = int64(2 << 20)
)

// HTTPClientRequest is outbound work scheduled by kbrd.http.request. The Lua
// host only builds and later consumes this value; schedulers perform the I/O.
type HTTPClientRequest struct {
	Token            string
	URL              string
	Method           string
	Headers          map[string][]string
	Body             string
	Timeout          time.Duration
	MaxResponseBytes int64
	DecodeJSON       bool
}

// HTTPClientResult is returned by ExecuteHTTP and handed back to the Lua
// callback. Non-2xx status codes are successful HTTP exchanges; Error is only
// set for transport, limit, or requested JSON-decoding failures.
type HTTPClientResult struct {
	Status      int
	Headers     map[string][]string
	Body        string
	URL         string
	JSON        any
	JSONDecoded bool
	Error       string
}

func (h *Host) installHTTPClientAPI(kbrd *lua.LTable) {
	httpAPI := h.L.NewTable()
	httpAPI.RawSetString("request", h.L.NewFunction(h.luaHTTPRequest))
	kbrd.RawSetString("http", httpAPI)
}

// kbrd.http.request(opts, fn) → handle | nil, err
func (h *Host) luaHTTPRequest(L *lua.LState) int {
	if h.inTimer {
		return errResult(L, fmt.Errorf("kbrd.http.request: cannot start async work from inside a timer callback"))
	}
	opts := L.CheckTable(1)
	fn := L.CheckFunction(2)
	req, err := h.decodeHTTPClientRequest(opts)
	if err != nil {
		return errResult(L, fmt.Errorf("kbrd.http.request: %w", err))
	}
	req.Token = h.allocToken()
	callback := ownedFn{fn: fn, owner: h.activeOwner}
	if h.stage != nil {
		h.stage.httpCallbacks[req.Token] = callback
		h.stage.pendingHTTP = append(h.stage.pendingHTTP, req)
	} else {
		h.httpCallbacks[req.Token] = callback
		h.pendingHTTPRequests = append(h.pendingHTTPRequests, req)
	}
	L.Push(lua.LString(req.Token))
	return 1
}

func (h *Host) decodeHTTPClientRequest(opts *lua.LTable) (HTTPClientRequest, error) {
	rawURL := lua.LVAsString(opts.RawGetString("url"))
	if rawURL == "" {
		return HTTPClientRequest{}, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return HTTPClientRequest{}, fmt.Errorf("url must be an absolute http:// or https:// URL")
	}

	method := strings.ToUpper(lua.LVAsString(opts.RawGetString("method")))
	if method == "" {
		method = http.MethodGet
	}
	// Let net/http apply its complete method-token validation before work is
	// queued, so malformed options fail synchronously in Lua.
	if _, err := http.NewRequest(method, rawURL, nil); err != nil {
		return HTTPClientRequest{}, fmt.Errorf("invalid request: %w", err)
	}

	headers, err := decodeHTTPClientHeaders(opts.RawGetString("headers"))
	if err != nil {
		return HTTPClientRequest{}, err
	}
	validationReq, _ := http.NewRequest(method, rawURL, nil)
	for name, values := range headers {
		for _, value := range values {
			validationReq.Header.Add(name, value)
		}
	}
	if err := validationReq.Write(io.Discard); err != nil {
		return HTTPClientRequest{}, fmt.Errorf("invalid headers: %w", err)
	}

	bodyValue := opts.RawGetString("body")
	jsonValue := opts.RawGetString("json")
	if bodyValue != lua.LNil && jsonValue != lua.LNil {
		return HTTPClientRequest{}, fmt.Errorf("body and json are mutually exclusive")
	}
	body := ""
	if bodyValue != lua.LNil {
		text, ok := bodyValue.(lua.LString)
		if !ok {
			return HTTPClientRequest{}, fmt.Errorf("body must be a string")
		}
		body = string(text)
	}
	if jsonValue != lua.LNil {
		value, err := h.luaToJSON(jsonValue, make(map[*lua.LTable]bool), 0)
		if err != nil {
			return HTTPClientRequest{}, fmt.Errorf("encode json: %w", err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return HTTPClientRequest{}, fmt.Errorf("encode json: %w", err)
		}
		body = string(encoded)
		if headerValue(headers, "Content-Type") == "" {
			headers[http.CanonicalHeaderKey("Content-Type")] = []string{"application/json"}
		}
	}

	timeout := time.Duration(h.cfg.HTTPTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	if value := opts.RawGetString("timeout_ms"); value != lua.LNil {
		n, ok := value.(lua.LNumber)
		if !ok || n <= 0 || float64(n) != math.Trunc(float64(n)) {
			return HTTPClientRequest{}, fmt.Errorf("timeout_ms must be a positive integer")
		}
		if float64(n) > float64(timeout.Milliseconds()) {
			return HTTPClientRequest{}, fmt.Errorf("timeout_ms exceeds configured maximum of %d", timeout.Milliseconds())
		}
		requested := time.Duration(int64(n)) * time.Millisecond
		timeout = requested
	}
	maxBytes := int64(h.cfg.HTTPMaxResponseBytes)
	if maxBytes <= 0 {
		maxBytes = defaultHTTPMaxResponseBytes
	}

	decodeJSON := false
	if value := opts.RawGetString("decode_json"); value != lua.LNil {
		flag, ok := value.(lua.LBool)
		if !ok {
			return HTTPClientRequest{}, fmt.Errorf("decode_json must be a boolean")
		}
		decodeJSON = bool(flag)
	}
	return HTTPClientRequest{
		URL: rawURL, Method: method, Headers: headers, Body: body,
		Timeout: timeout, MaxResponseBytes: maxBytes, DecodeJSON: decodeJSON,
	}, nil
}

func decodeHTTPClientHeaders(value lua.LValue) (map[string][]string, error) {
	if value == lua.LNil {
		return make(map[string][]string), nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("headers must be a table")
	}
	out := make(map[string][]string)
	var decodeErr error
	table.ForEach(func(key, value lua.LValue) {
		if decodeErr != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok || string(name) == "" {
			decodeErr = fmt.Errorf("header names must be non-empty strings")
			return
		}
		canonical := http.CanonicalHeaderKey(string(name))
		switch v := value.(type) {
		case lua.LString:
			out[canonical] = []string{string(v)}
		case *lua.LTable:
			count := 0
			validArray := true
			v.ForEach(func(key, _ lua.LValue) {
				count++
				if index, ok := exactPositiveIndex(key); !ok || index > v.Len() {
					validArray = false
				}
			})
			if !validArray || count != v.Len() {
				decodeErr = fmt.Errorf("header %q values must be a contiguous array", name)
				return
			}
			values := make([]string, 0, v.Len())
			for i := 1; i <= v.Len(); i++ {
				item, ok := v.RawGetInt(i).(lua.LString)
				if !ok {
					decodeErr = fmt.Errorf("header %q values must be strings", name)
					return
				}
				values = append(values, string(item))
			}
			out[canonical] = values
		default:
			decodeErr = fmt.Errorf("header %q must be a string or array of strings", name)
		}
	})
	return out, decodeErr
}

func headerValue(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// PendingHTTP drains outbound requests queued since the previous scheduler
// pass. Network calls must never execute on the Lua-owning goroutine.
func (h *Host) PendingHTTP() []HTTPClientRequest {
	if h == nil {
		return nil
	}
	out := h.pendingHTTPRequests
	h.pendingHTTPRequests = nil
	return out
}

// FireHTTP invokes the callback for a completed outbound request.
func (h *Host) FireHTTP(token string, result HTTPClientResult) error {
	if h == nil || h.L == nil {
		return nil
	}
	owned, ok := h.httpCallbacks[token]
	if !ok {
		return nil
	}
	delete(h.httpCallbacks, token)

	payload := h.L.NewTable()
	payload.RawSetString("ok", lua.LBool(result.Error == ""))
	payload.RawSetString("status", lua.LNumber(result.Status))
	payload.RawSetString("headers", toLValue(h.L, headersToIface(result.Headers)))
	payload.RawSetString("body", lua.LString(result.Body))
	payload.RawSetString("url", lua.LString(result.URL))
	if result.Error != "" {
		payload.RawSetString("error", lua.LString(result.Error))
	}
	if result.JSONDecoded {
		decoded, err := h.jsonToLua(result.JSON, 0)
		if err != nil {
			payload.RawSetString("ok", lua.LFalse)
			payload.RawSetString("error", lua.LString("decode JSON response: "+err.Error()))
		} else {
			payload.RawSetString("json", decoded)
		}
	}

	prevOwner := h.activeOwner
	h.activeOwner = owned.owner
	defer func() { h.activeOwner = prevOwner }()
	h.running = true
	defer func() {
		h.running = false
		pending := h.deferred
		h.deferred = nil
		for _, ev := range pending {
			h.OnEvent(ev)
		}
	}()
	if _, err := h.callHookLValue(owned.fn, payload, 0); err != nil {
		h.logger.Log("error", "http "+token, err.Error())
		h.api.Notify("http: "+err.Error(), "error")
		return err
	}
	return nil
}

// ExecuteHTTP performs one bounded request. It is safe to call from any worker
// goroutine and contains no Lua state.
func ExecuteHTTP(ctx context.Context, req HTTPClientRequest) HTTPClientResult {
	return executeHTTP(ctx, http.DefaultClient, req)
}

func executeHTTP(ctx context.Context, client *http.Client, req HTTPClientRequest) HTTPClientResult {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, req.Method, req.URL, strings.NewReader(req.Body))
	if err != nil {
		return HTTPClientResult{URL: req.URL, Error: err.Error()}
	}
	for name, values := range req.Headers {
		for _, value := range values {
			httpReq.Header.Add(name, value)
		}
	}

	response, err := client.Do(httpReq)
	if err != nil {
		return HTTPClientResult{URL: req.URL, Error: err.Error()}
	}
	defer response.Body.Close()

	maxBytes := req.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = defaultHTTPMaxResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	result := HTTPClientResult{
		Status: response.StatusCode, Headers: response.Header.Clone(),
		URL: req.URL,
	}
	if response.Request != nil && response.Request.URL != nil {
		result.URL = response.Request.URL.String()
	}
	if err != nil {
		result.Error = fmt.Sprintf("read response body: %v", err)
		return result
	}
	if int64(len(body)) > maxBytes {
		result.Body = string(body[:maxBytes])
		result.Error = fmt.Sprintf("response body exceeds limit of %d bytes", maxBytes)
		return result
	}
	result.Body = string(body)
	if req.DecodeJSON {
		value, err := decodeJSON(body)
		if err != nil {
			result.Error = fmt.Sprintf("decode JSON response: %v", err)
			return result
		}
		result.JSON = value
		result.JSONDecoded = true
	}
	return result
}
