package web

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func unreachableRequest(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("192.168.122.255"), Port: 7400}, Err: &os.SyscallError{Syscall: "connect", Err: syscall.ENETUNREACH}}
}

func TestFRPCConnectionErrorIsConciseOnDashboardAndDetail(t *testing.T) {
	s, err := New(config.Config{Instances: []config.Instance{{Name: "debXMR", URL: "http://192.168.122.255:7400"}}}, &http.Client{Transport: roundTripFunc(unreachableRequest)})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/instances/debXMR"} {
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, "Store API unreachable: 192.168.122.255:7400: network is unreachable") || strings.Contains(body, "Store API request:") || strings.Contains(body, "/api/store/proxies") {
			t.Fatalf("%s: status %d, body %q", path, response.Code, body)
		}
	}
}

func TestFRPCHTTPErrorKeepsServerDiagnostic(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer api.Close()
	s, err := New(config.Config{Instances: []config.Instance{{Name: "edge", URL: api.URL}}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Could not read Store API: Store API GET /api/store/proxies: unauthorized") {
		t.Fatalf("dashboard = %d %q", response.Code, response.Body.String())
	}
}

func TestFRPCStatusConnectionErrorKeepsProxyList(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"proxies":[{"name":"ssh","type":"tcp","tcp":{"localIP":"127.0.0.1","localPort":22,"remotePort":6000}}]}`)
	}))
	defer api.Close()
	s, err := New(config.Config{Instances: []config.Instance{{Name: "edge", URL: api.URL}}}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/status" {
			return unreachableRequest(r)
		}
		return http.DefaultTransport.RoundTrip(r)
	})})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "Runtime status unavailable: "+api.Listener.Addr().String()+": network is unreachable") || !strings.Contains(body, ">ssh<") {
		t.Fatalf("dashboard = %d %q", response.Code, body)
	}
}
