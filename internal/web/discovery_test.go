package web

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lanjelin/frpd/internal/config"
	"github.com/Lanjelin/frpd/internal/frps"
)

func TestDiscoverySeparatesUnmatchedClientsAndOfflineProxies(t *testing.T) {
	frpcAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/store/proxies" {
			t.Errorf("unexpected frpc path %s", r.URL.Path)
			return
		}
		_, _ = io.WriteString(w, `{"proxies":[{"name":"ssh","type":"tcp"}]}`)
	}))
	defer frpcAPI.Close()
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/clients":
			_, _ = io.WriteString(w, `[{"key":"linked-key","user":"team","clientID":"linked","online":true},{"key":"spare-key","user":"team","clientID":"spare","hostname":"router","online":true},{"key":"empty-key","user":"other","clientID":"empty","online":true}]`)
		case "/api/proxy/tcp":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"edge.ssh","user":"team","clientID":"linked","status":"online"},{"name":"remote","user":"team","clientID":"spare","status":"online","todayTrafficIn":2048},{"name":"old","user":"other","clientID":"old-client","status":"offline"}]}`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
				_, _ = io.WriteString(w, `{"proxies":[]}`)
			} else {
				t.Errorf("unexpected frps path %s", r.URL.Path)
			}
		}
	}))
	defer frpsAPI.Close()

	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}}, Instances: []config.Instance{{Name: "edge", ClientID: "linked", URL: frpcAPI.URL, FRPS: "public"}}}, frpsAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/frps/discovery", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("discovery status = %d: %s", response.Code, body)
	}
	for _, expected := range []string{`aria-label="Client spare"`, `aria-label="Client empty"`, `aria-label="Client old-client"`, `>remote<`, `2.0 KiB`, `data-frps-offline="true"`, `Host: router`} {
		if !strings.Contains(body, expected) {
			t.Errorf("missing %q in discovery: %s", expected, body)
		}
	}
	for _, absent := range []string{`aria-label="Client linked"`, `>edge.ssh<`, `>Edit<`, `>Delete<`} {
		if strings.Contains(body, absent) {
			t.Errorf("unexpected %q in discovery: %s", absent, body)
		}
	}
}
func TestDiscoveryExcludesConfiguredClientByIDWhenProxyNamesDiffer(t *testing.T) {
	frpcAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/store/proxies" {
			t.Errorf("unexpected frpc path %s", r.URL.Path)
			return
		}
		_, _ = io.WriteString(w, `{"proxies":[{"name":"portal","type":"http"}]}`)
	}))
	defer frpcAPI.Close()
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/clients":
			_, _ = io.WriteString(w, `[{"user":"arch","clientID":"arch","hostname":"archbtw","online":true},{"user":"guest","clientID":"visitor","online":true}]`)
		case "/api/proxy/http":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"arch.other-portal","user":"arch","clientID":"arch","status":"online"},{"name":"visitor-web","user":"guest","clientID":"visitor","status":"online"}]}`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
				_, _ = io.WriteString(w, `{"proxies":[]}`)
			} else {
				t.Errorf("unexpected frps path %s", r.URL.Path)
			}
		}
	}))
	defer frpsAPI.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}}, Instances: []config.Instance{{Name: "Arch laptop", ClientID: "arch", URL: frpcAPI.URL, FRPS: "public"}}}, frpsAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/frps/discovery", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("discovery status = %d: %s", response.Code, body)
	}
	if strings.Contains(body, `aria-label="Client arch"`) || strings.Contains(body, ">arch.other-portal<") {
		t.Errorf("configured client or its proxy appeared in unmatched discovery: %s", body)
	}
	if !strings.Contains(body, `aria-label="Client visitor"`) || !strings.Contains(body, ">visitor-web<") {
		t.Errorf("unconfigured client missing from discovery: %s", body)
	}
}

func TestDiscoveryDoesNotGuessWhichUserOwnsSharedClientID(t *testing.T) {
	frpcAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer frpcAPI.Close()
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/clients":
			_, _ = io.WriteString(w, `[{"user":"one","clientID":"edge","online":true},{"user":"two","clientID":"edge","online":true}]`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
				_, _ = io.WriteString(w, `{"proxies":[]}`)
			} else {
				t.Errorf("unexpected frps path %s", r.URL.Path)
			}
		}
	}))
	defer frpsAPI.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}}, Instances: []config.Instance{{Name: "Display edge", ClientID: "edge", URL: frpcAPI.URL, FRPS: "public"}}}, frpsAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	view := s.discoverFRPS(context.Background(), "public", s.frpsServers["public"])
	if view.Error != "" || len(view.Clients) != 2 {
		t.Fatalf("ambiguous clients must remain visible: %+v", view)
	}
}

func TestDiscoveryDisplaysAvailableFRPSDomains(t *testing.T) {
	for _, test := range []struct {
		name      string
		host      string
		httpPort  int64
		httpsPort int64
	}{
		{name: "server subdomain host", host: "example.net", httpPort: 8080, httpsPort: 8443},
		{name: "no server subdomain host", httpPort: 8080},
		{name: "unset vhost ports", host: "example.net"},
	} {
		t.Run(test.name, func(t *testing.T) {
			frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/clients":
					_, _ = io.WriteString(w, `[{"clientID":"external","online":true}]`)
				case "/api/serverinfo":
					_, _ = fmt.Fprintf(w, `{"subdomainHost":%q,"vhostHTTPPort":%d,"vhostHTTPSPort":%d}`, test.host, test.httpPort, test.httpsPort)
				case "/api/proxy/http":
					_, _ = io.WriteString(w, `{"proxies":[{"name":"web","clientID":"external","status":"online","conf":{"customDomains":["one.example.org","two.example.org"],"subdomain":"lab"}},{"name":"gone","clientID":"external","status":"offline","conf":null}]}`)
				case "/api/proxy/https":
					_, _ = io.WriteString(w, `{"proxies":[{"name":"secure","clientID":"external","status":"online","conf":{"subdomain":"secure"}}]}`)
				case "/api/proxy/tcpmux":
					_, _ = io.WriteString(w, `{"proxies":[{"name":"mux","clientID":"external","status":"online","conf":{"customDomains":["mux.example.org"]}}]}`)
				default:
					if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
						_, _ = io.WriteString(w, `{"proxies":[]}`)
					} else {
						t.Errorf("unexpected frps path %s", r.URL.Path)
					}
				}
			}))
			defer frpsAPI.Close()
			s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}}}, frpsAPI.Client())
			if err != nil {
				t.Fatal(err)
			}
			view := s.discoverFRPS(context.Background(), "public", s.frpsServers["public"])
			if view.Error != "" || len(view.Clients) != 1 {
				t.Fatalf("discovered clients: %+v", view)
			}
			remotes := make(map[string]string)
			for _, proxy := range view.Clients[0].Proxies {
				remotes[proxy.Name] = proxy.Remote
			}
			webDomain := "one.example.org"
			secondDomain := "two.example.org"
			subdomain := "subdomain: lab"
			secure := "subdomain: secure"
			if test.host != "" {
				subdomain = "lab." + test.host
				secure = "secure." + test.host
			}
			if test.httpPort > 0 {
				webDomain += ":8080"
				secondDomain += ":8080"
				if test.host != "" {
					subdomain += ":8080"
				}
			}
			if test.httpsPort > 0 && test.host != "" {
				secure += ":8443"
			}
			expected := map[string]string{
				"web":    webDomain + ", " + secondDomain + ", " + subdomain,
				"mux":    "mux.example.org",
				"gone":   "—",
				"secure": secure,
			}
			for name, remote := range expected {
				if remotes[name] != remote {
					t.Errorf("%s remote = %q, want %q", name, remotes[name], remote)
				}
			}
		})
	}
}

func TestDiscoveryFetchesVhostPortWithoutSubdomain(t *testing.T) {
	var infoRequests int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/clients":
			_, _ = io.WriteString(w, `[{"clientID":"external","online":true}]`)
		case "/api/serverinfo":
			infoRequests++
			_, _ = io.WriteString(w, `{"vhostHTTPPort":8081}`)
		case "/api/proxy/http":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"plain","clientID":"external","status":"online","conf":{"customDomains":["plain.example.com"]}}]}`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
				_, _ = io.WriteString(w, `{"proxies":[]}`)
			} else {
				t.Errorf("unexpected frps path %s", r.URL.Path)
			}
		}
	}))
	defer api.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: api.URL}}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	view := s.discoverFRPS(context.Background(), "public", s.frpsServers["public"])
	if view.Error != "" || len(view.Clients) != 1 || len(view.Clients[0].Proxies) != 1 {
		t.Fatalf("discovered clients: %+v", view)
	}
	if proxy := view.Clients[0].Proxies[0]; proxy.Remote != "plain.example.com:8081" || proxy.VhostHTTPPort != 8081 || infoRequests != 1 {
		t.Errorf("proxy = %+v, server info requests = %d", proxy, infoRequests)
	}
}

func TestDiscoveryCombinesFRPSHostAndTCPUDPRemotePorts(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/clients":
			_, _ = io.WriteString(w, `[{"clientID":"external","online":true}]`)
		case "/api/proxy/tcp":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"ssh","clientID":"external","status":"online","conf":{"remotePort":6000}},{"name":"closed","clientID":"external","status":"offline","conf":null}]}`)
		case "/api/proxy/udp":
			_, _ = io.WriteString(w, `{"proxies":[{"name":"dns","clientID":"external","status":"online","conf":{"remotePort":5353}}]}`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/proxy/") {
				_, _ = io.WriteString(w, `{"proxies":[]}`)
			} else {
				t.Errorf("unexpected frps path %s", r.URL.Path)
			}
		}
	}))
	defer api.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: api.URL}}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	view := s.discoverFRPS(context.Background(), "public", s.frpsServers["public"])
	if view.Error != "" || len(view.Clients) != 1 {
		t.Fatalf("discovered clients: %+v", view)
	}
	host, _, err := net.SplitHostPort(strings.TrimPrefix(api.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"ssh":    net.JoinHostPort(host, "6000"),
		"dns":    net.JoinHostPort(host, "5353"),
		"closed": "—",
	}
	for _, proxy := range view.Clients[0].Proxies {
		if proxy.Remote != expected[proxy.Name] {
			t.Errorf("%s remote = %q, want %q", proxy.Name, proxy.Remote, expected[proxy.Name])
		}
		delete(expected, proxy.Name)
	}
	if len(expected) != 0 {
		t.Errorf("missing proxies: %v", expected)
	}
}

func TestFRPSRemoteIPv6AndMissingHostname(t *testing.T) {
	proxy := frps.ProxyMetric{Type: "tcp"}
	proxy.Conf.RemotePort = 7000
	if got := frpsRemote(proxy, frps.ServerInfo{}, "2001:db8::1"); got != "[2001:db8::1]:7000" {
		t.Errorf("IPv6 remote = %q", got)
	}
	if got := frpsRemote(proxy, frps.ServerInfo{}, ""); got != "7000" {
		t.Errorf("port-only remote = %q", got)
	}
}

func TestPruneOfflineProxiesIsServerWideAndRequiresPost(t *testing.T) {
	var calls int
	frpsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.RequestURI() != "/api/proxies?status=offline" {
			t.Errorf("unexpected prune request %s %s", r.Method, r.URL.RequestURI())
		}
		calls++
		_, _ = io.WriteString(w, `{"code":200,"msg":"success"}`)
	}))
	defer frpsAPI.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "public", URL: frpsAPI.URL}}}, frpsAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRecorder()
	s.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/frps/public/prune", nil))
	if get.Code != http.StatusMethodNotAllowed || calls != 0 {
		t.Fatalf("GET prune status = %d, calls = %d", get.Code, calls)
	}
	unknown := httptest.NewRecorder()
	s.Handler().ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/frps/unknown/prune", nil))
	if unknown.Code != http.StatusNotFound || calls != 0 {
		t.Fatalf("unknown prune status = %d, calls = %d", unknown.Code, calls)
	}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/frps/public/prune", nil))
	if response.Code != http.StatusSeeOther || calls != 1 || !strings.Contains(response.Header().Get("Location"), "Cleared+offline+proxy+statistics") {
		t.Fatalf("prune status = %d, calls = %d, location = %q", response.Code, calls, response.Header().Get("Location"))
	}
}
