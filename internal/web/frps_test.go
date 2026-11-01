package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestDashboardSummarizesOptionalFRPSMetrics(t *testing.T) {
	frpcAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/store/proxies":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"web","type":"https","https":{"localIP":"127.0.0.1","localPort":443}}]}`)
		case "/api/status":
			_, _ = io.WriteString(w, `{"https":[{"name":"web","type":"https","status":"running"}]}`)
		default:
			t.Fatalf("unexpected frpc route %s", r.URL.Path)
		}
	}))
	defer frpcAPI.Close()
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/serverinfo":
			_, _ = io.WriteString(w, `{"version":"0.71.0","clientCounts":1}`)
		case "/api/proxy/https":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"web","clientID":"visitor","todayTrafficIn":7340032,"status":"online"},{"name":"device-7.web","clientID":"device-7","todayTrafficIn":1048576,"todayTrafficOut":2097152,"curConns":3,"status":"online"}]}`)
		default:
			t.Fatalf("unexpected frps route %s", r.URL.Path)
		}
	}))
	defer frpsAPI.Close()

	s, err := New(config.Config{
		FRPSServers: []config.FRPSServer{{Name: "edge", URL: frpsAPI.URL}},
		Instances:   []config.Instance{{Name: "client", ClientID: "device-7", URL: frpcAPI.URL, FRPS: "edge"}},
	}, frpcAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, expected := range []string{"FRPS", "1.0 MiB", "2.0 MiB", "3 conns"} {
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("dashboard missing %q: %d %q", expected, response.Code, response.Body.String())
		}
	}
	detail := httptest.NewRecorder()
	s.Handler().ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/instances/client", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "1.0 MiB") {
		t.Fatalf("detail = %d %q", detail.Code, detail.Body.String())
	}
}
