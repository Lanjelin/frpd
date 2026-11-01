package frps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestServerInfoReportsConciseHTMLErrors(t *testing.T) {
	for _, contentType := range []string{"text/html; charset=utf-8", "text/plain"} {
		t.Run(contentType, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("<!DOCTYPE html>\n<html><body><h1>Not Found</h1></body></html>"))
			}))
			defer api.Close()

			_, err := New(config.FRPSServer{URL: api.URL}, api.Client()).ServerInfo(context.Background())
			if err == nil || !strings.Contains(err.Error(), "GET /api/serverinfo: 404 Not Found") || strings.Contains(err.Error(), "DOCTYPE") || strings.Contains(err.Error(), "<html>") {
				t.Fatalf("ServerInfo error = %v", err)
			}
		})
	}
}

func TestServerInfoPreservesShortTextError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized\nextra diagnostic line"))
	}))
	defer api.Close()

	_, err := New(config.FRPSServer{URL: api.URL}, api.Client()).ServerInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GET /api/serverinfo: 401 Unauthorized: unauthorized") || strings.Contains(err.Error(), "extra diagnostic") {
		t.Fatalf("ServerInfo error = %v", err)
	}
}

func TestPruneOfflineUsesServerWideFRPSEndpoint(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "secret" || r.Method != http.MethodDelete || r.URL.RequestURI() != "/api/proxies?status=offline" {
			t.Errorf("unexpected FRPS prune request %s %s with user %q", r.Method, r.URL.RequestURI(), user)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer api.Close()

	err := New(config.FRPSServer{URL: api.URL, Username: "admin", Password: "secret"}, api.Client()).PruneOffline(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DELETE /api/proxies?status=offline: 403 Forbidden: forbidden") {
		t.Fatalf("PruneOffline error = %v", err)
	}
}

func TestAllProxiesQueriesTypesConcurrentlyAndKeepsTypeOrder(t *testing.T) {
	const delay = 150 * time.Millisecond
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		proxyType := strings.TrimPrefix(r.URL.Path, "/api/proxy/")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"proxies":[{"name":"%s-1"}]}`, proxyType))
	}))
	defer api.Close()

	started := time.Now()
	proxies, err := New(config.FRPSServer{URL: api.URL}, api.Client()).AllProxies(context.Background())
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 4*delay {
		t.Fatalf("AllProxies took %v for %d types; calls are not concurrent", elapsed, len(proxyTypes))
	}
	if len(proxies) != len(proxyTypes) {
		t.Fatalf("proxies = %#v", proxies)
	}
	for i, proxyType := range proxyTypes {
		if proxies[i].Type != proxyType || proxies[i].Name != proxyType+"-1" {
			t.Fatalf("proxies[%d] = %+v, want type %q", i, proxies[i], proxyType)
		}
	}
}

func TestAllProxiesReportsEarliestFailingType(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/proxy/stcp":
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "stcp broke")
		case "/api/proxy/http":
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "http broke")
		default:
			_, _ = io.WriteString(w, `{"proxies":[]}`)
		}
	}))
	defer api.Close()

	_, err := New(config.FRPSServer{URL: api.URL}, api.Client()).AllProxies(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/api/proxy/http") || strings.Contains(err.Error(), "stcp") {
		t.Fatalf("AllProxies error = %v, want the http failure (earliest type), not the faster stcp one", err)
	}
}
