package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestMutationsRejectCrossOriginBrowserRequests(t *testing.T) {
	tests := []struct {
		name      string
		allowed   []string
		headers   map[string]string
		wantAllow bool
	}{
		{"no browser headers (curl)", nil, nil, true},
		{"same-origin fetch metadata", nil, map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"none fetch metadata", nil, map[string]string{"Sec-Fetch-Site": "none"}, true},
		{"cross-site fetch metadata", nil, map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"same-site fetch metadata", nil, map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{"matching origin over plain http", nil, map[string]string{"Origin": "http://dash.lan:8080"}, true},
		{"mismatched origin", nil, map[string]string{"Origin": "http://evil.example"}, false},
		{"null origin", nil, map[string]string{"Origin": "null"}, false},
		{"same-origin metadata wins over odd origin", nil, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://proxy.example"}, true},
		{"cross-site metadata wins over matching origin", nil, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://dash.lan:8080"}, false},
		{"allowed origin behind host-rewriting proxy", []string{"http://frp.lan"}, map[string]string{"Origin": "http://frp.lan"}, true},
		{"allowed origin matches case and default port", []string{"HTTPS://Frp.Example.com:443"}, map[string]string{"Origin": "https://frp.example.com"}, true},
		{"allowed origin overrides cross-site metadata", []string{"https://portal.example"}, map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://portal.example"}, true},
		{"allowlist does not admit other origins", []string{"http://frp.lan"}, map[string]string{"Origin": "http://evil.example"}, false},
		{"allowlist does not match different scheme", []string{"https://frp.lan"}, map[string]string{"Origin": "http://frp.lan"}, false},
		{"allowlist does not match different port", []string{"http://frp.lan:8080"}, map[string]string{"Origin": "http://frp.lan:9090"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var deletes atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deletes.Add(1)
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			defer api.Close()
			s, err := New(config.Config{AllowedOrigins: test.allowed, Instances: []config.Instance{{Name: "edge", URL: api.URL}}}, api.Client())
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "http://dash.lan:8080/instances/edge/proxies/ssh/delete", nil)
			for key, value := range test.headers {
				request.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			s.Handler().ServeHTTP(recorder, request)
			if test.wantAllow {
				if recorder.Code != http.StatusSeeOther || deletes.Load() != 1 {
					t.Fatalf("allowed request = %d, upstream deletes = %d", recorder.Code, deletes.Load())
				}
				return
			}
			if recorder.Code != http.StatusForbidden || deletes.Load() != 0 {
				t.Fatalf("blocked request = %d, upstream deletes = %d", recorder.Code, deletes.Load())
			}
		})
	}
}

func TestCrossOriginGuardDoesNotBlockReads(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer api.Close()
	s, err := New(config.Config{Instances: []config.Instance{{Name: "edge", URL: api.URL}}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cross-site GET = %d", recorder.Code)
	}
	if recorder.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("missing anti-framing headers: %v", recorder.Header())
	}
}

func TestAuthChallengeKeepsAntiFramingHeaders(t *testing.T) {
	s, err := New(config.Config{Username: "admin", Password: "secret"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("unauthenticated = %d %v", recorder.Code, recorder.Header())
	}
}

func TestDashboardLoadsInstancesConcurrentlyInConfiguredOrder(t *testing.T) {
	const delay = 300 * time.Millisecond
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer api.Close()
	s, err := New(config.Config{Instances: []config.Instance{
		{Name: "bravo", URL: api.URL},
		{Name: "alpha", URL: api.URL},
		{Name: "charlie", URL: api.URL},
	}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	elapsed := time.Since(started)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard = %d", recorder.Code)
	}
	// Each instance makes two sequential upstream calls (list + status), so
	// serial loading of three instances would take at least 6*delay.
	if elapsed > 4*delay {
		t.Fatalf("dashboard took %v; instances are not loaded concurrently", elapsed)
	}
	body := recorder.Body.String()
	alpha, bravo, charlie := strings.Index(body, "alpha"), strings.Index(body, "bravo"), strings.Index(body, "charlie")
	if alpha < 0 || !(alpha < bravo && bravo < charlie) {
		t.Fatalf("instances not in name order: alpha=%d bravo=%d charlie=%d", alpha, bravo, charlie)
	}
}

func TestInstancesSharingFRPSQueryServerInfoOncePerRequest(t *testing.T) {
	var serverInfoCalls atomic.Int32
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/serverinfo" {
			serverInfoCalls.Add(1)
			_, _ = io.WriteString(w, `{"version":"0.68.0"}`)
			return
		}
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer frpsAPI.Close()
	frpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer frpc.Close()
	s, err := New(config.Config{
		FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}},
		Instances: []config.Instance{
			{Name: "one", URL: frpc.URL, ClientID: "a", FRPS: "public"},
			{Name: "two", URL: frpc.URL, ClientID: "b", FRPS: "public"},
		},
	}, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || serverInfoCalls.Load() != 1 {
		t.Fatalf("dashboard = %d, serverinfo calls = %d", recorder.Code, serverInfoCalls.Load())
	}
}
