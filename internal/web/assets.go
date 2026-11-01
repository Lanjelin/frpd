package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// hashAssets returns a short content hash for every file directly under fsys.
func hashAssets(fsys fs.FS) (map[string]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("list static assets: %w", err)
	}
	hashes := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read static asset %q: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		hashes[entry.Name()] = hex.EncodeToString(sum[:])[:12]
	}
	return hashes, nil
}

// assetURL is the template function behind {{asset "app.js"}}. It returns a
// versioned URL, and an error for unknown names so typos fail at render time.
func assetURL(hashes map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		hash, ok := hashes[name]
		if !ok {
			return "", fmt.Errorf("unknown static asset %q", name)
		}
		return "/static/" + name + "?v=" + hash, nil
	}
}

// staticHandler serves fsys under /static/. Every file gets an ETag from its
// content hash, so revalidation is cheap. A request whose ?v= matches the
// current hash is cached for a year; anything else is revalidated each time.
func staticHandler(fsys fs.FS, hashes map[string]string) http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/static/")
		header := w.Header()
		header.Set("Cache-Control", "no-cache")
		if hash, ok := hashes[name]; ok {
			header.Set("ETag", `"`+hash+`"`)
			if r.URL.Query().Get("v") == hash {
				header.Set("Cache-Control", "public, max-age=31536000, immutable")
			}
		}
		if name == "site.webmanifest" {
			header.Set("Content-Type", "application/manifest+json")
		}
		files.ServeHTTP(w, r)
	})
}
