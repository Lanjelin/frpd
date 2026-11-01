package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Lanjelin/frpd/internal/config"
)

func TestHashAssetsChangesWithContentOnly(t *testing.T) {
	first, err := hashAssets(fstest.MapFS{"app.js": {Data: []byte("one")}, "styles.css": {Data: []byte("same")}, "sub/ignored.js": {Data: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashAssets(fstest.MapFS{"app.js": {Data: []byte("two")}, "styles.css": {Data: []byte("same")}})
	if err != nil {
		t.Fatal(err)
	}
	if first["app.js"] == second["app.js"] || first["app.js"] == "" {
		t.Fatalf("app.js hash did not change with content: %q vs %q", first["app.js"], second["app.js"])
	}
	if first["styles.css"] != second["styles.css"] {
		t.Fatalf("unchanged file got different hashes: %q vs %q", first["styles.css"], second["styles.css"])
	}
	if _, ok := first["ignored.js"]; ok {
		t.Fatal("hashed a file inside a subdirectory")
	}
}

func TestPagesReferenceHashedAssetsServedWithMatchingCachePolicy(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"proxies":[]}`)
	}))
	defer api.Close()
	s, err := New(config.Config{Instances: []config.Instance{{Name: "edge", URL: api.URL}}}, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	get := func(path string, headers ...string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			request.Header.Set(headers[i], headers[i+1])
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	pages := []string{"/", "/instances/edge", "/instances/edge/proxies/new", "/settings", "/frps/discovery"}
	reference := regexp.MustCompile(`/static/[A-Za-z0-9_.-]+\?v=[0-9a-f]{12}`)
	var assetPath string
	for _, page := range pages {
		response := get(page)
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %q", page, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if strings.Contains(body, "?v=4\"") || strings.Contains(body, "?v=10\"") || strings.Contains(body, "?v=13\"") || strings.Contains(body, "?v=5\"") {
			t.Fatalf("%s still has a hand-written asset version", page)
		}
		if page == "/frps/discovery" {
			continue
		}
		if strings.Contains(body, `"/static/app.js"`) || !reference.MatchString(body) {
			t.Fatalf("%s lacks hashed static references: %q", page, body)
		}
		if page == "/" {
			assetPath = regexp.MustCompile(`/static/app\.js\?v=[0-9a-f]{12}`).FindString(body)
		}
	}
	if assetPath == "" {
		t.Fatal("dashboard has no hashed app.js reference")
	}

	asset := get(assetPath)
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Cache-Control"), "immutable") || asset.Header().Get("ETag") == "" {
		t.Fatalf("versioned asset = %d, headers %v", asset.Code, asset.Header())
	}
	if cached := get(assetPath, "If-None-Match", asset.Header().Get("ETag")); cached.Code != http.StatusNotModified {
		t.Fatalf("revalidating with ETag = %d, want 304", cached.Code)
	}
	stale := get("/static/app.js?v=000000000000")
	if stale.Header().Get("Cache-Control") != "no-cache" || stale.Header().Get("ETag") != asset.Header().Get("ETag") {
		t.Fatalf("stale version must not be cached as immutable: %v", stale.Header())
	}
	bare := get("/static/site.webmanifest")
	if bare.Header().Get("Cache-Control") != "no-cache" || bare.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("unversioned manifest headers = %v", bare.Header())
	}
}
