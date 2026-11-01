package web

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lanjelin/frpd/internal/config"
)

// fakeFRPC is an in-memory frpc Store API.
type fakeFRPC struct {
	*httptest.Server
	delay        time.Duration
	mu           sync.Mutex
	proxies      map[string]map[string]any
	writes       int
	inflight     int
	maxInflight  int
	rejectWrites string
}

func newFakeFRPC(t *testing.T, proxies ...map[string]any) *fakeFRPC {
	t.Helper()
	f := &fakeFRPC{proxies: make(map[string]map[string]any)}
	for _, proxy := range proxies {
		f.store(proxy)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/store/proxies", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		list := make([]map[string]any, 0, len(f.proxies))
		for _, proxy := range f.proxies {
			list = append(list, proxy)
		}
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"proxies": list})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	mux.HandleFunc("GET /api/store/proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		proxy, ok := f.proxies[r.PathValue("name")]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "proxy not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, proxy)
	})
	mux.HandleFunc("POST /api/store/proxies", func(w http.ResponseWriter, r *http.Request) {
		proxy := f.decode(t, r)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes++
		if f.rejectWrites != "" {
			http.Error(w, f.rejectWrites, http.StatusInternalServerError)
			return
		}
		name, _ := proxy["name"].(string)
		if _, exists := f.proxies[name]; exists {
			http.Error(w, "proxy already exists", http.StatusConflict)
			return
		}
		f.proxies[name] = proxy
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /api/store/proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		proxy := f.decode(t, r)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes++
		if f.rejectWrites != "" {
			http.Error(w, f.rejectWrites, http.StatusInternalServerError)
			return
		}
		if _, exists := f.proxies[r.PathValue("name")]; !exists {
			http.Error(w, "proxy not found", http.StatusNotFound)
			return
		}
		f.proxies[r.PathValue("name")] = proxy
	})
	mux.HandleFunc("DELETE /api/store/proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes++
		if f.rejectWrites != "" {
			http.Error(w, f.rejectWrites, http.StatusInternalServerError)
			return
		}
		delete(f.proxies, r.PathValue("name"))
	})
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.inflight++
		f.maxInflight = max(f.maxInflight, f.inflight)
		delay := f.delay
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			f.inflight--
			f.mu.Unlock()
		}()
		time.Sleep(delay)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// store normalizes proxy through JSON so numbers are float64 like real requests.
func (f *fakeFRPC) store(proxy map[string]any) {
	data, _ := json.Marshal(proxy)
	var normalized map[string]any
	_ = json.Unmarshal(data, &normalized)
	f.proxies[normalized["name"].(string)] = normalized
}

func (f *fakeFRPC) decode(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var proxy map[string]any
	if err := json.NewDecoder(r.Body).Decode(&proxy); err != nil {
		t.Errorf("fake frpc got invalid JSON body: %v", err)
	}
	return proxy
}

func (f *fakeFRPC) proxy(name string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	proxy, ok := f.proxies[name]
	return proxy, ok
}

func (f *fakeFRPC) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func sshProxy() map[string]any {
	return map[string]any{
		"name":   "ssh",
		"type":   "tcp",
		"custom": map[string]any{"keep": true},
		"tcp":    map[string]any{"localIP": "127.0.0.1", "localPort": 22, "remotePort": 6000},
	}
}

func dashboardFor(t *testing.T, instances map[string]*fakeFRPC) http.Handler {
	t.Helper()
	cfg := config.Config{}
	for name, frpc := range instances {
		cfg.Instances = append(cfg.Instances, config.Instance{Name: name, URL: frpc.URL})
	}
	s, err := New(cfg, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func submit(handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func fetch(handler http.Handler, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

// redirectQuery asserts a 303 to "/" and returns its query.
func redirectQuery(t *testing.T, recorder *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %q", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || location.Path != "/" {
		t.Fatalf("Location = %q (%v)", recorder.Header().Get("Location"), err)
	}
	return location.Query()
}

func TestCreateProxyRejectsInvalidDefinitionsBeforeCallingFRPC(t *testing.T) {
	tests := []struct {
		name, body, wantError string
	}{
		{"not JSON", "not json", "proxy must be a JSON object"},
		{"blank name", `{"name":" ","type":"tcp","tcp":{}}`, "non-empty name"},
		{"missing type", `{"name":"a","tcp":{}}`, "requires a type"},
		{"missing type object", `{"name":"a","type":"tcp"}`, "configuration object matching its type"},
		{"type object has wrong shape", `{"name":"a","type":"tcp","tcp":"22"}`, "configuration object matching its type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frpc := newFakeFRPC(t)
			handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
			recorder := submit(handler, "/instances/edge/proxies", url.Values{"proxy": {test.body}})
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), test.wantError) {
				t.Fatalf("status = %d, want form re-rendered with %q; body %q", recorder.Code, test.wantError, recorder.Body.String())
			}
			if frpc.writeCount() != 0 {
				t.Fatalf("frpc received %d writes for an invalid definition", frpc.writeCount())
			}
		})
	}
}

func TestCreateProxyStoresDefinitionWithUnknownFieldsAndRedirects(t *testing.T) {
	frpc := newFakeFRPC(t)
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	body := `{"name":"web","type":"tcp","futureField":{"x":1},"tcp":{"localPort":80,"remotePort":8080}}`
	query := redirectQuery(t, submit(handler, "/instances/edge/proxies", url.Values{"proxy": {body}}))
	if query.Get("message") != "Proxy created" {
		t.Fatalf("redirect query = %v", query)
	}
	stored, ok := frpc.proxy("web")
	if !ok || stored["futureField"] == nil || stored["tcp"].(map[string]any)["remotePort"] != float64(8080) {
		t.Fatalf("stored proxy = %#v", stored)
	}
}

func TestCreateProxyShowsUpstreamRejectionInForm(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy())
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	recorder := submit(handler, "/instances/edge/proxies", url.Values{"proxy": {`{"name":"ssh","type":"tcp","tcp":{}}`}})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "proxy already exists") {
		t.Fatalf("status = %d, body %q", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateProxyReplacesStoredDefinitionForEscapedName(t *testing.T) {
	frpc := newFakeFRPC(t, map[string]any{"name": "my proxy", "type": "tcp", "tcp": map[string]any{"localPort": 1}})
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	body := `{"name":"my proxy","type":"tcp","tcp":{"localPort":2222}}`
	query := redirectQuery(t, submit(handler, "/instances/edge/proxies/my%20proxy", url.Values{"proxy": {body}}))
	if query.Get("message") != "Proxy updated" {
		t.Fatalf("redirect query = %v", query)
	}
	stored, _ := frpc.proxy("my proxy")
	if stored["tcp"].(map[string]any)["localPort"] != float64(2222) {
		t.Fatalf("stored proxy = %#v", stored)
	}
}

func TestUpdateProxyFailureRerendersEditFormForSameProxy(t *testing.T) {
	frpc := newFakeFRPC(t)
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	recorder := submit(handler, "/instances/edge/proxies/ghost", url.Values{"proxy": {`{"name":"ghost","type":"tcp","tcp":{}}`}})
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "proxy not found") || !strings.Contains(body, `action="/instances/edge/proxies/ghost"`) {
		t.Fatalf("status = %d, body %q", recorder.Code, body)
	}
}

func TestEditProxyRendersStoredDefinitionOrRedirectsWithError(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy())
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	found := fetch(handler, "/instances/edge/proxies/ssh/edit")
	if found.Code != http.StatusOK || !strings.Contains(found.Body.String(), "&#34;localPort&#34;: 22") {
		t.Fatalf("edit page = %d %q", found.Code, found.Body.String())
	}
	missing := fetch(handler, "/instances/edge/proxies/ghost/edit")
	if got := redirectQuery(t, missing).Get("error"); !strings.Contains(got, "proxy not found") {
		t.Fatalf("missing proxy error = %q", got)
	}
}

func TestProxyRoutesReturn404ForUnknownInstance(t *testing.T) {
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": newFakeFRPC(t)})
	form := url.Values{"proxy": {`{"name":"a","type":"tcp","tcp":{}}`}, "enabled": {"true"}}
	for _, path := range []string{"/instances/nope", "/instances/nope/proxies/new", "/instances/nope/proxies/a/edit"} {
		if code := fetch(handler, path).Code; code != http.StatusNotFound {
			t.Fatalf("GET %s = %d", path, code)
		}
	}
	for _, path := range []string{"/instances/nope/proxies", "/instances/nope/proxies/a", "/instances/nope/proxies/a/toggle", "/instances/nope/proxies/a/delete"} {
		if code := submit(handler, path, form).Code; code != http.StatusNotFound {
			t.Fatalf("POST %s = %d", path, code)
		}
	}
}

func TestNewProxyFormStartsFromTCPSkeleton(t *testing.T) {
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": newFakeFRPC(t)})
	recorder := fetch(handler, "/instances/edge/proxies/new")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `action="/instances/edge/proxies"`) || !strings.Contains(recorder.Body.String(), "&#34;type&#34;: &#34;tcp&#34;") {
		t.Fatalf("new proxy page = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestToggleChangesOnlyEnabledAndKeepsEverythingElse(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy())
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	query := redirectQuery(t, submit(handler, "/instances/edge/proxies/ssh/toggle", url.Values{"enabled": {"false"}}))
	if query.Get("message") != "Proxy disabled" {
		t.Fatalf("redirect query = %v", query)
	}
	stored, _ := frpc.proxy("ssh")
	tcp := stored["tcp"].(map[string]any)
	if tcp["enabled"] != false || tcp["localPort"] != float64(22) || tcp["remotePort"] != float64(6000) || stored["custom"] == nil {
		t.Fatalf("stored proxy after disable = %#v", stored)
	}
	query = redirectQuery(t, submit(handler, "/instances/edge/proxies/ssh/toggle", url.Values{"enabled": {"true"}}))
	if stored, _ = frpc.proxy("ssh"); query.Get("message") != "Proxy enabled" || stored["tcp"].(map[string]any)["enabled"] != true {
		t.Fatalf("after enable: query %v, stored %#v", query, stored)
	}
}

func TestToggleRejectsBadInputWithoutWriting(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy(), map[string]any{"name": "broken", "type": "tcp"})
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	tests := []struct {
		name, path string
		form       url.Values
		wantError  string
	}{
		{"invalid enabled value", "/instances/edge/proxies/ssh/toggle", url.Values{"enabled": {"maybe"}}, "invalid enabled value"},
		{"missing enabled value", "/instances/edge/proxies/ssh/toggle", url.Values{}, "invalid enabled value"},
		{"unknown proxy", "/instances/edge/proxies/ghost/toggle", url.Values{"enabled": {"true"}}, "proxy not found"},
		{"proxy without type object", "/instances/edge/proxies/broken/toggle", url.Values{"enabled": {"true"}}, "configuration object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redirectQuery(t, submit(handler, test.path, test.form)).Get("error"); !strings.Contains(got, test.wantError) {
				t.Fatalf("error = %q, want %q", got, test.wantError)
			}
		})
	}
	if frpc.writeCount() != 0 {
		t.Fatalf("frpc received %d writes", frpc.writeCount())
	}
}

func TestDeleteProxyRemovesItOrReportsUpstreamFailure(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy())
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	frpc.rejectWrites = "store is read-only"
	if got := redirectQuery(t, submit(handler, "/instances/edge/proxies/ssh/delete", nil)).Get("error"); !strings.Contains(got, "store is read-only") {
		t.Fatalf("failure error = %q", got)
	}
	if _, ok := frpc.proxy("ssh"); !ok {
		t.Fatal("proxy was removed despite upstream failure")
	}
	frpc.rejectWrites = ""
	if got := redirectQuery(t, submit(handler, "/instances/edge/proxies/ssh/delete", nil)).Get("message"); got != "Proxy deleted" {
		t.Fatalf("message = %q", got)
	}
	if _, ok := frpc.proxy("ssh"); ok {
		t.Fatal("proxy still stored after delete")
	}
}

func TestSettingsListsUniqueVhostPortsAndIgnoresUnreachableFRPS(t *testing.T) {
	serve := func(body string, status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
	}
	first := serve(`{"vhostHTTPPort":8080,"vhostHTTPSPort":443}`, http.StatusOK)
	second := serve(`{"vhostHTTPPort":80,"vhostHTTPSPort":443}`, http.StatusOK)
	broken := serve("down", http.StatusInternalServerError)
	defer first.Close()
	defer second.Close()
	defer broken.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{
		{Name: "a", URL: first.URL}, {Name: "b", URL: second.URL}, {Name: "c", URL: broken.URL},
	}}, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	recorder := fetch(s.Handler(), "/settings")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "80, 443, 8080") {
		t.Fatalf("settings = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestMutationsOnOneInstanceDoNotOverlap(t *testing.T) {
	edge := newFakeFRPC(t, sshProxy(), map[string]any{"name": "web", "type": "tcp", "tcp": map[string]any{}})
	edge.delay = 100 * time.Millisecond
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": edge})
	requests := []struct {
		path string
		form url.Values
	}{
		{"/instances/edge/proxies/ssh/toggle", url.Values{"enabled": {"false"}}},
		{"/instances/edge/proxies/ssh", url.Values{"proxy": {`{"name":"ssh","type":"tcp","tcp":{"localPort":2222}}`}}},
		{"/instances/edge/proxies/web/delete", nil},
	}
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if recorder := submit(handler, request.path, request.form); recorder.Code != http.StatusSeeOther {
				t.Errorf("POST %s = %d %q", request.path, recorder.Code, recorder.Body.String())
			}
		}()
	}
	wg.Wait()
	if edge.maxInflight != 1 {
		t.Fatalf("frpc saw %d simultaneous requests; mutations on one instance must be serialized", edge.maxInflight)
	}
}

func TestMutationsOnDifferentInstancesRunInParallel(t *testing.T) {
	one, two := newFakeFRPC(t, sshProxy()), newFakeFRPC(t, sshProxy())
	one.delay, two.delay = 300*time.Millisecond, 300*time.Millisecond
	handler := dashboardFor(t, map[string]*fakeFRPC{"one": one, "two": two})
	started := time.Now()
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			submit(handler, "/instances/"+name+"/proxies/ssh/delete", nil)
		}()
	}
	wg.Wait()
	if elapsed := time.Since(started); elapsed > 550*time.Millisecond {
		t.Fatalf("two instances took %v; one instance's lock must not block another", elapsed)
	}
}

func TestFRPSDiscoveryQueriesServersConcurrently(t *testing.T) {
	const delay = 300 * time.Millisecond
	serve := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			if r.URL.Path == "/api/clients" {
				_, _ = io.WriteString(w, `[]`)
				return
			}
			_, _ = io.WriteString(w, `{"proxies":[]}`)
		}))
	}
	alpha, beta := serve(), serve()
	defer alpha.Close()
	defer beta.Close()
	s, err := New(config.Config{FRPSServers: []config.FRPSServer{{Name: "beta", URL: beta.URL}, {Name: "alpha", URL: alpha.URL}}}, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	recorder := fetch(s.Handler(), "/frps/discovery")
	elapsed := time.Since(started)
	// Fully serial would be 2 servers x 9 calls x delay = 5.4s.
	if recorder.Code != http.StatusOK || elapsed > 4*delay {
		t.Fatalf("discovery = %d in %v", recorder.Code, elapsed)
	}
	body := recorder.Body.String()
	if a, b := strings.Index(body, "alpha"), strings.Index(body, "beta"); a < 0 || b < a {
		t.Fatalf("servers not sorted by name: alpha=%d beta=%d", a, b)
	}
}

// TestEmbeddedUIIsCSPCompatible keeps the UI loadable under contentSecurityPolicy.
// Inline scripts, inline style attributes and inline event handlers would be
// blocked in browsers, so fail here instead of silently breaking the UI.
func TestEmbeddedUIIsCSPCompatible(t *testing.T) {
	if strings.Contains(contentSecurityPolicy, "unsafe-") {
		t.Fatalf("CSP allows unsafe sources: %s", contentSecurityPolicy)
	}
	forbidden := map[string]*regexp.Regexp{
		"inline <script> body":       regexp.MustCompile(`<script(\s[^>]*)?>\s*[^<\s]`),
		"inline style attribute":     regexp.MustCompile(`\sstyle\s*=`),
		"inline event handler":       regexp.MustCompile(`\son[a-z]+\s*=\s*["']`),
		"javascript: URL":            regexp.MustCompile(`javascript:`),
		"style set via setAttribute": regexp.MustCompile(`setAttribute\(\s*["']style["']`),
		"<style> element":            regexp.MustCompile(`<style[\s>]`),
	}
	checked := 0
	for _, pattern := range []string{"templates/*.html", "static/*.js"} {
		files, err := fs.Glob(assets, pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("glob %s: %v (%d files)", pattern, err, len(files))
		}
		for _, name := range files {
			data, err := fs.ReadFile(assets, name)
			if err != nil {
				t.Fatal(err)
			}
			for what, re := range forbidden {
				if match := re.Find(data); match != nil {
					t.Errorf("%s contains %s: %q", name, what, match)
				}
			}
			checked++
		}
	}
	if checked < 5 {
		t.Fatalf("only checked %d files", checked)
	}
}

func TestDeleteFormsCarryConfirmationForTheEmbeddedScript(t *testing.T) {
	frpc := newFakeFRPC(t, sshProxy())
	handler := dashboardFor(t, map[string]*fakeFRPC{"edge": frpc})
	for _, page := range []string{"/", "/instances/edge"} {
		body := fetch(handler, page).Body.String()
		form := regexp.MustCompile(`<form[^>]*action="/instances/edge/proxies/ssh/delete"[^>]*>`).FindString(body)
		if form == "" || !strings.Contains(form, `data-confirm="Delete proxy ssh?"`) {
			t.Fatalf("%s: delete form = %q; it must carry data-confirm so app.js can ask before deleting", page, form)
		}
	}
	script, err := fs.ReadFile(assets, "static/app.js")
	if err != nil || !strings.Contains(string(script), "[data-confirm]") {
		t.Fatalf("app.js does not handle [data-confirm]: %v", err)
	}
}
