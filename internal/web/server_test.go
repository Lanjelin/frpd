package web

import (
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestDashboardShowsRuntimeStatePluginEndpointAndToggle(t *testing.T) {
	var updated string
	proxies := `{"proxies":[
		{"name":"ssh","type":"tcp","tcp":{"localIP":"127.0.0.1","localPort":22,"remotePort":6000}},
		{"name":"web","type":"https","https":{"enabled":false,"customDomains":["app.example.com"],"plugin":{"type":"http2https","localAddr":"192.168.50.239:3001","requestHeaders":{}}}},
		{"name":"offline","type":"tcp","tcp":{"localIP":"127.0.0.1","localPort":8080,"remotePort":7000}}
	]}`
	storeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/store/proxies":
			_, _ = io.WriteString(w, proxies)
		case r.Method == http.MethodGet && r.URL.Path == "/api/status":
			_, _ = io.WriteString(w, `{"tcp":[{"name":"ssh","type":"tcp","status":"running","local_addr":"127.0.0.1:22","remote_addr":"relay.example.com:6000","source":"store"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/store/proxies":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/api/store/proxies/ssh":
			_, _ = io.WriteString(w, `{"name":"ssh","type":"tcp","tcp":{"localIP":"127.0.0.1","localPort":22,"remotePort":6000}}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/store/proxies/ssh":
			body, _ := io.ReadAll(r.Body)
			updated = string(body)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer storeAPI.Close()

	s, err := New(config.Config{Instances: []config.Instance{{Name: "edge", URL: storeAPI.URL}}}, storeAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	dashboard := httptest.NewRecorder()
	s.Handler().ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/", nil))
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "relay.example.com:6000") || !strings.Contains(dashboard.Body.String(), "192.168.50.239:3001") || !strings.Contains(dashboard.Body.String(), ">http2https<") || !strings.Contains(dashboard.Body.String(), `data-proxy-type="https"`) {
		t.Fatalf("dashboard = %d %q", dashboard.Code, dashboard.Body.String())
	}
	detail := httptest.NewRecorder()
	s.Handler().ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/instances/edge", nil))
	body := detail.Body.String()
	for _, expected := range []string{"running", "relay.example.com:6000", "192.168.50.239:3001", "Disabled", "Not running"} {
		if detail.Code != http.StatusOK || !strings.Contains(body, expected) {
			t.Fatalf("detail missing %q: %d %q", expected, detail.Code, body)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/instances/edge/proxies/ssh/toggle", strings.NewReader(url.Values{"enabled": {"false"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("toggle status = %d", response.Code)
	}
	if !strings.Contains(updated, `"enabled":false`) {
		t.Fatalf("toggle request = %s", updated)
	}
}

func TestBasicAuthProtectsDashboardAssetsAndMutations(t *testing.T) {
	s, err := New(config.Config{
		Username:  "admin",
		Password:  "secret",
		Instances: []config.Instance{{Name: "edge", URL: "http://127.0.0.1:1"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	for _, path := range []string{"/settings", "/static/app.js", "/frps/discovery", "/instances/edge/proxies/ssh/toggle"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/toggle") {
			method = http.MethodPost
		}
		for _, credentials := range []struct{ username, password string }{
			{},
			{"admin", "wrong"},
			{"wrong", "secret"},
		} {
			request := httptest.NewRequest(method, path, nil)
			if credentials.username != "" {
				request.SetBasicAuth(credentials.username, credentials.password)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || !strings.HasPrefix(response.Header().Get("WWW-Authenticate"), `Basic realm="frpd"`) {
				t.Fatalf("%s %s with user %q: status %d, challenge %q", method, path, credentials.username, response.Code, response.Header().Get("WWW-Authenticate"))
			}
		}
	}
	for _, path := range []string{"/settings", "/static/app.js"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.SetBasicAuth("admin", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("authorized %s: status %d", path, response.Code)
		}
	}

	unprotected, err := New(config.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	unprotected.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if response.Code != http.StatusOK || response.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("unconfigured auth: status %d, challenge %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
}

func TestDashboardOrdersConfiguredTunnelPanels(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/store/proxies":
			_, _ = io.WriteString(w, `{"proxies":[]}`)
		case "/api/status":
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	path := filepath.Join(t.TempDir(), "frpd.toml")
	content := `[[frpc]]
name = "Unordered B"
url = "` + api.URL + `"
[[frpc]]
name = "Ordered B"
order = 0
url = "` + api.URL + `"
[[frpc]]
name = "Ordered A"
order = 0
url = "` + api.URL + `"
[[frpc]]
name = "First"
order = -1
url = "` + api.URL + `"
[[frpc]]
name = "Unordered A"
url = "` + api.URL + `"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	last := -1
	for _, name := range []string{"First", "Ordered A", "Ordered B", "Unordered A", "Unordered B"} {
		index := strings.Index(body, `data-instance="`+name+`"`)
		if index <= last {
			t.Fatalf("dashboard panel %q at %d, previous at %d", name, index, last)
		}
		last = index
	}
}

func TestRenderExecutionFailureWritesSingleServerError(t *testing.T) {
	server := Server{templates: template.Must(template.New("bad").Parse(`{{template "missing"}}`))}
	response := httptest.NewRecorder()

	server.render(response, "bad", nil)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("render status = %d", response.Code)
	}
	if response.Body.String() != "internal server error\n" {
		t.Fatalf("render body = %q", response.Body.String())
	}
}
