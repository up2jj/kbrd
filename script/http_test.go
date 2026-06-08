package script

import (
	"testing"
)

func sampleReq() HTTPRequestData {
	return HTTPRequestData{
		Method:     "GET",
		Path:       "/c/todo",
		RawQuery:   "q=1",
		Headers:    map[string][]string{"Cookie": {"a=1", "b=2"}},
		RemoteAddr: "1.2.3.4:5555",
	}
}

func TestFireHTTPRequestNoHook(t *testing.T) {
	dir := writeInit(t, `kbrd.on("board_load", function() end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPRequest(sampleReq())
	if v.Action != "" || v.Skipped {
		t.Fatalf("expected zero verdict, got %+v", v)
	}
}

func TestFireHTTPRequestRespond(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("http_request", function(req)
			if req.path == "/c/todo" and req.query == "q=1" then
				return { action="respond", status=403, body="no", headers={["X-A"]="1"} }
			end
			return nil
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPRequest(sampleReq())
	if v.Action != "respond" || v.Status != 403 || v.Body != "no" || v.Headers["X-A"] != "1" {
		t.Fatalf("unexpected verdict: %+v", v)
	}
}

func TestFireHTTPRequestRedirect(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("http_request", function(req)
			return { action="redirect", location="/login", status=302 }
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPRequest(sampleReq())
	if v.Action != "redirect" || v.Location != "/login" || v.Status != 302 {
		t.Fatalf("unexpected verdict: %+v", v)
	}
}

func TestFireHTTPRequestContinueRewrite(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("http_request", function(req)
			return { action="continue", rewrite={
				path="/", query="y=2",
				set_headers={["X-Tag"]="1"}, del_headers={"Cookie"},
			} }
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPRequest(sampleReq())
	if v.Action != "continue" || v.Rewrite == nil {
		t.Fatalf("expected continue+rewrite, got %+v", v)
	}
	if v.Rewrite.Path != "/" || v.Rewrite.Query != "y=2" {
		t.Fatalf("unexpected rewrite path/query: %+v", v.Rewrite)
	}
	if v.Rewrite.SetHeaders["X-Tag"] != "1" || len(v.Rewrite.DelHeaders) != 1 || v.Rewrite.DelHeaders[0] != "Cookie" {
		t.Fatalf("unexpected rewrite headers: %+v", v.Rewrite)
	}
}

// The hook must see repeated header values as a Lua array.
func TestFireHTTPRequestHeadersMultiValue(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("http_request", function(req)
			kbrd.notify("cookies:" .. #req.headers["Cookie"], "info")
			return nil
		end)`)
	api := &fakeAPI{}
	h, err := New(defaultCfg(), api, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	h.FireHTTPRequest(sampleReq())
	if len(api.notifies) != 1 || api.notifies[0] != "info:cookies:2" {
		t.Fatalf("expected cookies:2, got %v", api.notifies)
	}
}

func TestFireHTTPRequestSkippedWhileRunning(t *testing.T) {
	dir := writeInit(t, `kbrd.on("http_request", function(req) return { action="respond" } end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	h.running = true
	if v := h.FireHTTPRequest(sampleReq()); !v.Skipped {
		t.Fatalf("expected Skipped while running, got %+v", v)
	}
}

func TestFireHTTPResponseOverride(t *testing.T) {
	dir := writeInit(t, `
		kbrd.on("http_response", function(resp)
			if resp.status == 200 and resp.body == "<html>" then
				return { status=201, set_headers={["X-Hook"]="hi"}, body="rewritten" }
			end
			return nil
		end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPResponse(HTTPResponseData{
		Method: "GET", Path: "/", Status: 200,
		Headers: map[string][]string{"Content-Type": {"text/html"}}, Body: "<html>",
	})
	if !v.Changed || v.Status != 201 || v.SetHeaders["X-Hook"] != "hi" {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	if v.Body == nil || *v.Body != "rewritten" {
		t.Fatalf("expected body override, got %+v", v.Body)
	}
}

// An empty-string body override is distinct from "leave the body alone" (nil).
func TestFireHTTPResponseEmptyBodyOverride(t *testing.T) {
	dir := writeInit(t, `kbrd.on("http_response", function(resp) return { body="" } end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPResponse(HTTPResponseData{Status: 200, Body: "x"})
	if v.Body == nil || *v.Body != "" {
		t.Fatalf("expected empty-string body override, got %+v", v.Body)
	}
}

func TestFireHTTPResponseDeclineLeavesBody(t *testing.T) {
	dir := writeInit(t, `kbrd.on("http_response", function(resp) return nil end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	v := h.FireHTTPResponse(HTTPResponseData{Status: 200, Body: "x"})
	if v.Changed || v.Body != nil {
		t.Fatalf("expected unchanged verdict, got %+v", v)
	}
}

func TestHasHook(t *testing.T) {
	dir := writeInit(t, `kbrd.on("http_request", function(req) end)`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if !h.HasHook("http_request") {
		t.Fatal("expected http_request hook present")
	}
	if h.HasHook("http_response") {
		t.Fatal("expected no http_response hook")
	}
	var nilHost *Host
	if nilHost.HasHook("http_request") {
		t.Fatal("nil host must report no hooks")
	}
}
