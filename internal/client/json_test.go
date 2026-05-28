package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJSONClientVerifiesTLSByDefault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := newJSONClient(server.URL, time.Second, false, nil)
	if err != nil {
		t.Fatalf("newJSONClient() error = %v", err)
	}

	if err := client.do(http.MethodGet, "/chord/ping", nil, &struct{}{}); err == nil {
		t.Fatal("expected TLS verification error")
	}
}

func TestJSONClientCanSkipTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := newJSONClient(server.URL, time.Second, true, nil)
	if err != nil {
		t.Fatalf("newJSONClient() error = %v", err)
	}

	if err := client.do(http.MethodGet, "/chord/ping", nil, &struct{}{}); err != nil {
		t.Fatalf("client.do() error = %v", err)
	}
}
