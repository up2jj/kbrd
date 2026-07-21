package script

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHTTPRequestSchedulesAndDeliversDecodedJSON(t *testing.T) {
	dir := writeInit(t, `
request_handle, request_err = kbrd.http.request({
  url = "https://example.test/items",
  method = "post",
  headers = { ["X-Test"] = {"one", "two"} },
  json = {name="card", empty=kbrd.json.array()},
  decode_json = true,
  timeout_ms = 250,
}, function(res) http_result = res end)
`)
	cfg := defaultCfg()
	cfg.HTTPTimeoutMs = 1000
	cfg.HTTPMaxResponseBytes = 4096
	h, err := New(cfg, &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	pending := h.PendingHTTP()
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	req := pending[0]
	if req.Method != http.MethodPost || req.Timeout != 250*time.Millisecond || !req.DecodeJSON {
		t.Fatalf("request = %+v", req)
	}
	if req.Body != `{"empty":[],"name":"card"}` || headerValue(req.Headers, "Content-Type") != "application/json" {
		t.Fatalf("body/headers = %q, %#v", req.Body, req.Headers)
	}
	if len(req.Headers["X-Test"]) != 2 {
		t.Fatalf("repeated header = %#v", req.Headers["X-Test"])
	}

	decoded, err := decodeJSON([]byte(`{"id":7,"missing":null}`))
	if err != nil {
		t.Fatal(err)
	}
	err = h.FireHTTP(req.Token, HTTPClientResult{
		Status: 201, Headers: map[string][]string{"Set-Cookie": {"a=1", "b=2"}},
		Body: `{"id":7,"missing":null}`, URL: "https://example.test/items", JSON: decoded, JSONDecoded: true,
	})
	if err != nil {
		t.Fatalf("FireHTTP: %v", err)
	}
	result, ok := h.L.GetGlobal("http_result").(*lua.LTable)
	if !ok || !lua.LVAsBool(result.RawGetString("ok")) || int(lua.LVAsNumber(result.RawGetString("status"))) != 201 {
		t.Fatalf("result = %v", h.L.GetGlobal("http_result"))
	}
	jsonResult := result.RawGetString("json").(*lua.LTable)
	if int(lua.LVAsNumber(jsonResult.RawGetString("id"))) != 7 || jsonResult.RawGetString("missing") != h.jsonNull {
		t.Fatalf("decoded result = %v", jsonResult)
	}
}

func TestHTTPRequestRejectsBodyAndJSON(t *testing.T) {
	dir := writeInit(t, `
handle, request_err = kbrd.http.request({
  url="https://example.test", body="x", json={x=1},
}, function() end)
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()
	if h.L.GetGlobal("handle") != lua.LNil || !strings.Contains(lua.LVAsString(h.L.GetGlobal("request_err")), "mutually exclusive") {
		t.Fatalf("result = %v, %v", h.L.GetGlobal("handle"), h.L.GetGlobal("request_err"))
	}
	if len(h.PendingHTTP()) != 0 {
		t.Fatal("invalid request must not be scheduled")
	}
}

func TestExecuteHTTPMethodHeadersBodyAndJSON(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if req.Method != http.MethodPut || req.Header.Get("X-Test") != "yes" || string(body) != "payload" {
			t.Fatalf("request = %s %#v %q", req.Method, req.Header, body)
		}
		return &http.Response{
			StatusCode: 202,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"accepted":true}`)),
			Request:    req,
		}, nil
	})}
	result := executeHTTP(t.Context(), client, HTTPClientRequest{
		URL: "https://example.test/path", Method: http.MethodPut,
		Headers: map[string][]string{"X-Test": {"yes"}}, Body: "payload",
		Timeout: time.Second, MaxResponseBytes: 1024, DecodeJSON: true,
	})
	if result.Error != "" || result.Status != 202 || !result.JSONDecoded {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteHTTPInvalidJSONPreservesResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 502,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader("not-json")),
			Request:    req,
		}, nil
	})}
	result := executeHTTP(t.Context(), client, HTTPClientRequest{
		URL: "https://example.test", Method: http.MethodGet,
		MaxResponseBytes: 1024, DecodeJSON: true,
	})
	if result.Status != 502 || result.Body != "not-json" || !strings.Contains(result.Error, "decode JSON response") {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteHTTPBoundsTimeoutAndBody(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		result := executeHTTP(t.Context(), client, HTTPClientRequest{
			URL: "https://example.test", Method: http.MethodGet, Timeout: time.Millisecond,
		})
		if !strings.Contains(result.Error, "deadline exceeded") {
			t.Fatalf("error = %q", result.Error)
		}
	})

	t.Run("body limit", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("12345")), Request: req}, nil
		})}
		result := executeHTTP(context.Background(), client, HTTPClientRequest{
			URL: "https://example.test", Method: http.MethodGet, MaxResponseBytes: 4,
		})
		if !strings.Contains(result.Error, "exceeds limit") {
			t.Fatalf("error = %q", result.Error)
		}
	})
}

func TestHostCloseCancelsHTTPWorkContext(t *testing.T) {
	dir := writeInit(t, `kbrd.status("loaded")`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := h.WorkContext()
	h.Close()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("host close did not cancel HTTP work context")
	}
}
