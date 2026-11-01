package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestClientUsesStoreEnvelopeAndBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "secret" {
			t.Fatal("missing Store API credentials")
		}
		if r.URL.Path != "/api/store/proxies" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"proxies":[{"name":"ssh","type":"tcp","tcp":{"localIP":"127.0.0.1","localPort":22,"remotePort":6000}}]}`)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON content type")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := New(config.Instance{URL: server.URL, Username: "admin", Password: "secret"}, nil)
	proxies, err := client.ListProxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 1 || proxies[0]["name"] != "ssh" {
		t.Fatalf("proxies = %#v", proxies)
	}
	if err := client.CreateProxy(context.Background(), Proxy{"name": "ssh", "type": "tcp", "tcp": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestClientErrorsKeepOnlyBoundedFirstLineOfUpstreamBody(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
		deny   string
	}{
		{"plain message", http.StatusUnauthorized, "unauthorized", "Store API DELETE /api/store/proxies/ssh: unauthorized", ""},
		{"multi-line body keeps first line", http.StatusBadRequest, "name is required\nstack trace line", "name is required", "stack trace"},
		{"html page falls back to status", http.StatusNotFound, "<!doctype html>\n<html><body>nope</body></html>", "404 Not Found", "<html>"},
		{"empty body falls back to status", http.StatusBadGateway, "", "502 Bad Gateway", ""},
		{"huge body is bounded", http.StatusInternalServerError, strings.Repeat("é", 100000), "…", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			err := New(config.Instance{URL: server.URL}, nil).DeleteProxy(context.Background(), "ssh")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
			if test.deny != "" && strings.Contains(err.Error(), test.deny) {
				t.Fatalf("error leaks %q: %v", test.deny, err)
			}
			if len(err.Error()) > 400 {
				t.Fatalf("error is %d bytes; upstream body must be bounded", len(err.Error()))
			}
		})
	}
}
