package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/live" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	if code := healthcheck("0.0.0.0:" + port); code != 0 {
		t.Fatalf("healthy server reported %d", code)
	}
	server.Close()
	if code := healthcheck("127.0.0.1:" + port); code != 1 {
		t.Fatalf("stopped server reported %d", code)
	}
}
