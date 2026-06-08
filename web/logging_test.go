package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	s := &Server{}
	h := s.accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/c/todo", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot || rec.Body.String() != "hello" {
		t.Fatalf("response not passed through: code=%d body=%q", rec.Code, rec.Body.String())
	}

	line := buf.String()
	for _, want := range []string{"web: GET /c/todo", "418", "5B", "10.0.0.1"} {
		if !strings.Contains(line, want) {
			t.Errorf("access log %q missing %q", strings.TrimSpace(line), want)
		}
	}
}

func TestAccessLogImplicit200(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	s := &Server{}
	h := s.accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(buf.String(), "200") {
		t.Errorf("expected implicit 200 in log, got %q", strings.TrimSpace(buf.String()))
	}
}
