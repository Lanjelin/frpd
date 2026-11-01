package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Lanjelin/frpd/internal/config"
	"github.com/Lanjelin/frpd/internal/frps"
)

// contentSecurityPolicy forbids inline script and style. The UI is built from
// same-origin files only (see TestEmbeddedUIIsCSPCompatible); keep it that way
// instead of loosening this policy.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; " +
	"manifest-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// guardCrossOrigin sets anti-framing headers on every response and rejects
// state-changing requests that a browser marks as cross-site.
//
// An Origin listed in allowed (normalized, from allowedOrigins) always passes.
// Otherwise Sec-Fetch-Site is authoritative when present (browsers only send
// it over HTTPS or localhost). Otherwise Origin is compared with Host, which
// covers plain-HTTP LAN access. Requests with neither header (curl, scripts)
// pass.
func guardCrossOrigin(allowed map[string]struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		if !isSafeMethod(r.Method) && !isSameOrigin(r, allowed) {
			http.Error(w, "cross-origin request blocked", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func isSameOrigin(r *http.Request, allowed map[string]struct{}) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		if normalized, err := config.NormalizeOrigin(origin); err == nil {
			if _, ok := allowed[normalized]; ok {
				return true
			}
		}
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// serverInfoCache memoizes frps ServerInfo per client for one request, so
// instances sharing an FRPS server query it once.
type serverInfoCache struct {
	mu      sync.Mutex
	entries map[*frps.Client]*serverInfoEntry
}

type serverInfoEntry struct {
	once sync.Once
	info frps.ServerInfo
	err  error
}

func newServerInfoCache() *serverInfoCache {
	return &serverInfoCache{entries: make(map[*frps.Client]*serverInfoEntry)}
}

func (c *serverInfoCache) get(ctx context.Context, client *frps.Client) (frps.ServerInfo, error) {
	c.mu.Lock()
	entry, ok := c.entries[client]
	if !ok {
		entry = &serverInfoEntry{}
		c.entries[client] = entry
	}
	c.mu.Unlock()
	entry.once.Do(func() { entry.info, entry.err = client.ServerInfo(ctx) })
	return entry.info, entry.err
}
