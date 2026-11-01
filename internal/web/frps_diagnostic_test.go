package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestDashboardDisplaysFRPSConnectionFailure(t *testing.T) {
	frpcAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/store/proxies":
			_, _ = io.WriteString(w, `{"proxies":[]}`)
		case "/api/status":
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer frpcAPI.Close()
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer frpsAPI.Close()

	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "edge", URL: frpsAPI.URL}}, Instances: []config.Instance{{Name: "client", ClientID: "client-id", URL: frpcAPI.URL, FRPS: "edge"}}}, frpcAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "FRPS unavailable") || !strings.Contains(body, "unauthorized") {
		t.Fatalf("dashboard = %d %q", response.Code, body)
	}
}
