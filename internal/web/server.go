// Package web serves the frpc Store API dashboard.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lanjelin/frpd/internal/config"
	"github.com/Lanjelin/frpd/internal/frps"
	"github.com/Lanjelin/frpd/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Server struct {
	instances      map[string]*store.Client
	frpsByInstance map[string]*frps.Client
	clientIDs      map[string]string
	frpsServers    map[string]*frps.Client
	frpsHosts      map[string]string
	names          []string
	templates      *template.Template
	assetHashes    map[string]string
	instanceLocks  map[string]*sync.Mutex
	authEnabled    bool
	allowedOrigins map[string]struct{}
	authUsername   [sha256.Size]byte
	authPassword   [sha256.Size]byte
}

type instanceView struct {
	Name               string
	Proxies            []proxyView
	Error              string
	StatusError        string
	FRPSError          string
	FRPSConfigured     bool
	FRPSConnected      bool
	FRPSVersion        string
	FRPSClientCount    int64
	FRPSMetricsMatched int
	VhostHTTPPort      int64
	VhostHTTPSPort     int64
	Running            int
	Disabled           int
	NotRunning         int
	TrafficIn          string
	TrafficOut         string
	Connections        int64
}

type proxyView struct {
	Name           string
	Type           string
	DisplayType    string
	Local          string
	Remote         string
	State          string
	StateClass     string
	Detail         string
	Enabled        bool
	Instance       string
	TrafficIn      string
	TrafficOut     string
	TrafficTotal   int64
	Connections    int64
	ServerState    string
	VhostHTTPPort  int64
	VhostHTTPSPort int64
}

type dashboardPage struct {
	Instances []instanceView
	Message   string
	Error     string
}

type instancePage struct {
	Instance instanceView
}

type proxyPage struct {
	Instance string
	Name     string
	JSON     string
	IsEdit   bool
	Error    string
}

type settingsPage struct {
	VhostPorts string
}

func New(cfg config.Config, httpClient *http.Client) (*Server, error) {
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}
	assetHashes, err := hashAssets(staticFS)
	if err != nil {
		return nil, err
	}
	parsed, err := template.New("").Funcs(template.FuncMap{"asset": assetURL(assetHashes)}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	frpsClients := make(map[string]*frps.Client, len(cfg.FRPSServers))
	frpsHosts := make(map[string]string, len(cfg.FRPSServers))
	for _, server := range cfg.FRPSServers {
		frpsClients[server.Name] = frps.New(server, httpClient)
		if endpoint, err := url.Parse(server.URL); err == nil {
			frpsHosts[server.Name] = endpoint.Hostname()
		}
	}
	allowedOrigins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		normalized, err := config.NormalizeOrigin(origin)
		if err != nil {
			return nil, fmt.Errorf("allowedOrigins: %w", err)
		}
		allowedOrigins[normalized] = struct{}{}
	}
	s := &Server{
		instances:      make(map[string]*store.Client, len(cfg.Instances)),
		instanceLocks:  make(map[string]*sync.Mutex, len(cfg.Instances)),
		frpsByInstance: make(map[string]*frps.Client, len(cfg.Instances)),
		frpsServers:    frpsClients,
		frpsHosts:      frpsHosts,
		clientIDs:      make(map[string]string, len(cfg.Instances)),
		templates:      parsed,
		assetHashes:    assetHashes,
		authEnabled:    cfg.Username != "" && cfg.Password != "",
		allowedOrigins: allowedOrigins,
		authUsername:   sha256.Sum256([]byte(cfg.Username)),
		authPassword:   sha256.Sum256([]byte(cfg.Password)),
	}
	orders := make(map[string]*int, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		orders[instance.Name] = instance.Order
		s.clientIDs[instance.Name] = instance.ClientID
		s.instances[instance.Name] = store.New(instance, httpClient)
		s.instanceLocks[instance.Name] = &sync.Mutex{}
		if instance.FRPS != "" {
			s.frpsByInstance[instance.Name] = frpsClients[instance.FRPS]
		}
		s.names = append(s.names, instance.Name)
	}
	sort.Slice(s.names, func(i, j int) bool {
		left, right := orders[s.names[i]], orders[s.names[j]]
		if left == nil || right == nil {
			if left == nil && right == nil {
				return s.names[i] < s.names[j]
			}
			return left != nil
		}
		if *left != *right {
			return *left < *right
		}
		return s.names[i] < s.names[j]
	})
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", staticHandler(static, s.assetHashes))
	mux.HandleFunc("GET /", s.dashboard)
	mux.HandleFunc("GET /frps/discovery", s.frpsDiscovery)
	mux.HandleFunc("GET /frps/{server}/prune", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("POST /frps/{server}/prune", s.pruneFRPS)
	mux.HandleFunc("GET /settings", s.settings)
	mux.HandleFunc("GET /instances/{instance}", s.instanceDetail)
	mux.HandleFunc("GET /instances/{instance}/proxies/new", s.newProxy)
	mux.HandleFunc("POST /instances/{instance}/proxies", s.createProxy)
	mux.HandleFunc("GET /instances/{instance}/proxies/{name}/edit", s.editProxy)
	mux.HandleFunc("POST /instances/{instance}/proxies/{name}", s.updateProxy)
	mux.HandleFunc("POST /instances/{instance}/proxies/{name}/toggle", s.toggleProxy)
	mux.HandleFunc("POST /instances/{instance}/proxies/{name}/delete", s.deleteProxy)
	if !s.authEnabled {
		return guardCrossOrigin(s.allowedOrigins, mux)
	}
	return guardCrossOrigin(s.allowedOrigins, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		userHash := sha256.Sum256([]byte(username))
		passHash := sha256.Sum256([]byte(password))
		if !ok || subtle.ConstantTimeCompare(userHash[:], s.authUsername[:])&subtle.ConstantTimeCompare(passHash[:], s.authPassword[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="frpd", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	page := dashboardPage{Message: r.URL.Query().Get("message"), Error: r.URL.Query().Get("error")}
	page.Instances = s.loadInstances(ctx)
	s.render(w, "dashboard", page)
}

func (s *Server) instanceDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("instance")
	if _, ok := s.instances[name]; !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	s.render(w, "instance", instancePage{Instance: s.loadInstance(ctx, name, newServerInfoCache())})
}

// loadInstances loads every configured instance concurrently, preserving s.names order.
func (s *Server) loadInstances(ctx context.Context) []instanceView {
	views := make([]instanceView, len(s.names))
	cache := newServerInfoCache()
	var wg sync.WaitGroup
	for i, name := range s.names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			views[i] = s.loadInstance(ctx, name, cache)
		}()
	}
	wg.Wait()
	return views
}

func (s *Server) loadInstance(ctx context.Context, name string, cache *serverInfoCache) instanceView {
	view := instanceView{Name: name}
	proxies, err := s.instances[name].ListProxies(ctx)
	if err != nil {
		view.Error = "Could not read Store API: " + err.Error()
		if detail := connectionDetail(err); detail != "" {
			view.Error = "Store API unreachable: " + detail
		}
		return view
	}
	statuses, statusErr := s.instances[name].ProxyStatuses(ctx)
	if statusErr != nil {
		view.StatusError = "Could not read runtime status: " + statusErr.Error()
		if detail := connectionDetail(statusErr); detail != "" {
			view.StatusError = "Runtime status unavailable: " + detail
		}
	}
	for _, proxy := range proxies {
		status, found := statuses[stringValue(proxy, "name")]
		view.Proxies = append(view.Proxies, presentProxy(name, proxy, status, found, statusErr != nil))
	}
	sort.Slice(view.Proxies, func(i, j int) bool { return view.Proxies[i].Name < view.Proxies[j].Name })
	s.attachFRPSMetrics(ctx, &view, cache)
	for _, proxy := range view.Proxies {
		switch proxy.State {
		case "Disabled":
			view.Disabled++
		case "running":
			view.Running++
		default:
			view.NotRunning++
		}
	}
	return view
}

func connectionDetail(err error) string {
	var requestErr *url.Error
	if !errors.As(err, &requestErr) {
		return ""
	}
	target, err := url.Parse(requestErr.URL)
	if err != nil || target.Host == "" {
		return ""
	}
	cause := requestErr.Err
	var networkErr *net.OpError
	if errors.As(cause, &networkErr) {
		cause = networkErr.Err
	}
	var syscallErr *os.SyscallError
	if errors.As(cause, &syscallErr) {
		cause = syscallErr.Err
	}
	return target.Host + ": " + cause.Error()
}

func (s *Server) attachFRPSMetrics(ctx context.Context, view *instanceView, cache *serverInfoCache) {
	client, configured := s.frpsByInstance[view.Name]
	view.FRPSConfigured = configured
	if !configured {
		return
	}
	info, err := cache.get(ctx, client)
	if err != nil {
		view.FRPSError = err.Error()
		return
	}
	view.FRPSConnected = true
	view.FRPSVersion = info.Version
	view.FRPSClientCount = info.ClientCounts
	view.VhostHTTPPort = info.VhostHTTPPort
	view.VhostHTTPSPort = info.VhostHTTPSPort
	for index := range view.Proxies {
		view.Proxies[index].VhostHTTPPort = view.VhostHTTPPort
		view.Proxies[index].VhostHTTPSPort = view.VhostHTTPSPort
	}
	types := make(map[string]struct{})
	for _, proxy := range view.Proxies {
		types[proxy.Type] = struct{}{}
	}
	metrics := make(map[string]frps.ProxyMetric)
	for proxyType := range types {
		items, err := client.ProxiesByType(ctx, proxyType)
		if err != nil {
			view.FRPSError = err.Error()
			return
		}
		for _, item := range items {
			metrics[item.Name] = item
		}
	}
	var trafficIn, trafficOut int64
	clientID := s.clientIDs[view.Name]
	for i := range view.Proxies {
		metric, found := metrics[view.Proxies[i].Name]
		if !found || (metric.ClientID != "" && metric.ClientID != clientID) {
			metric, found = metrics[clientID+"."+view.Proxies[i].Name]
		}
		if !found || (metric.ClientID != "" && metric.ClientID != clientID) {
			continue
		}
		view.FRPSMetricsMatched++
		view.Proxies[i].TrafficIn = formatBytes(metric.TodayTrafficIn)
		view.Proxies[i].TrafficTotal = metric.TodayTrafficIn + metric.TodayTrafficOut
		view.Proxies[i].TrafficOut = formatBytes(metric.TodayTrafficOut)
		view.Proxies[i].Connections = metric.CurConns
		view.Proxies[i].ServerState = metric.Status
		trafficIn += metric.TodayTrafficIn
		trafficOut += metric.TodayTrafficOut
		view.Connections += metric.CurConns
	}
	view.TrafficIn = formatBytes(trafficIn)
	view.TrafficOut = formatBytes(trafficOut)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ports := make(map[int64]struct{})
	for _, client := range s.frpsServers {
		info, err := client.ServerInfo(ctx)
		if err != nil {
			continue
		}
		for _, port := range []int64{info.VhostHTTPPort, info.VhostHTTPSPort} {
			if port > 0 {
				ports[port] = struct{}{}
			}
		}
	}
	values := make([]int, 0, len(ports))
	for port := range ports {
		values = append(values, int(port))
	}
	sort.Ints(values)
	display := make([]string, len(values))
	for index, port := range values {
		display[index] = strconv.Itoa(port)
	}
	s.render(w, "settings", settingsPage{VhostPorts: strings.Join(display, ", ")})
}

func (s *Server) newProxy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.instance(r); !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, "proxy", proxyPage{Instance: r.PathValue("instance"), JSON: "{\n  \"name\": \"\",\n  \"type\": \"tcp\",\n  \"tcp\": {}\n}"})
}

func (s *Server) editProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := s.instance(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	proxy, err := client.GetProxy(r.Context(), r.PathValue("name"))
	if err != nil {
		s.redirectError(w, r, err)
		return
	}
	data, err := json.MarshalIndent(proxy, "", "  ")
	if err != nil {
		s.redirectError(w, r, err)
		return
	}
	s.render(w, "proxy", proxyPage{Instance: r.PathValue("instance"), Name: r.PathValue("name"), JSON: string(data), IsEdit: true})
}

func (s *Server) createProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := s.instance(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer s.lockInstance(r.PathValue("instance"))()
	proxy, err := parseProxy(r)
	if err == nil {
		err = client.CreateProxy(r.Context(), proxy)
	}
	if err != nil {
		s.render(w, "proxy", proxyPage{Instance: r.PathValue("instance"), JSON: r.FormValue("proxy"), Error: err.Error()})
		return
	}
	s.redirectMessage(w, r, "Proxy created")
}

func (s *Server) updateProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := s.instance(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer s.lockInstance(r.PathValue("instance"))()
	proxy, err := parseProxy(r)
	if err == nil {
		err = client.UpdateProxy(r.Context(), r.PathValue("name"), proxy)
	}
	if err != nil {
		s.render(w, "proxy", proxyPage{Instance: r.PathValue("instance"), Name: r.PathValue("name"), JSON: r.FormValue("proxy"), IsEdit: true, Error: err.Error()})
		return
	}
	s.redirectMessage(w, r, "Proxy updated")
}

func (s *Server) toggleProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := s.instance(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer s.lockInstance(r.PathValue("instance"))()
	enabled, err := strconv.ParseBool(r.FormValue("enabled"))
	if err != nil {
		s.redirectError(w, r, fmt.Errorf("invalid enabled value"))
		return
	}
	name := r.PathValue("name")
	proxy, err := client.GetProxy(r.Context(), name)
	if err == nil {
		proxyType := stringValue(proxy, "type")
		proxyConfig, valid := proxy[proxyType].(map[string]any)
		if !valid {
			err = fmt.Errorf("proxy %q has no %q configuration object", name, proxyType)
		} else {
			proxyConfig["enabled"] = enabled
			err = client.UpdateProxy(r.Context(), name, proxy)
		}
	}
	if err != nil {
		s.redirectError(w, r, err)
		return
	}
	if enabled {
		s.redirectMessage(w, r, "Proxy enabled")
	} else {
		s.redirectMessage(w, r, "Proxy disabled")
	}
}

func (s *Server) deleteProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := s.instance(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer s.lockInstance(r.PathValue("instance"))()
	if err := client.DeleteProxy(r.Context(), r.PathValue("name")); err != nil {
		s.redirectError(w, r, err)
		return
	}
	s.redirectMessage(w, r, "Proxy deleted")
}

func (s *Server) instance(r *http.Request) (*store.Client, bool) {
	client, ok := s.instances[r.PathValue("instance")]
	return client, ok
}

func parseProxy(r *http.Request) (store.Proxy, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("read form: %w", err)
	}
	var proxy store.Proxy
	if err := json.Unmarshal([]byte(r.FormValue("proxy")), &proxy); err != nil {
		return nil, fmt.Errorf("proxy must be a JSON object: %w", err)
	}
	name, _ := proxy["name"].(string)
	proxyType, _ := proxy["type"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("proxy requires a non-empty name")
	}
	if strings.TrimSpace(proxyType) == "" {
		return nil, fmt.Errorf("proxy requires a type")
	}
	if _, ok := proxy[proxyType].(map[string]any); !ok {
		return nil, fmt.Errorf("proxy requires a %q configuration object matching its type", proxyType)
	}
	return proxy, nil
}

func presentProxy(instance string, proxy store.Proxy, status store.ProxyStatus, foundStatus, statusUnavailable bool) proxyView {
	proxyType := stringValue(proxy, "type")
	config, _ := proxy[proxyType].(map[string]any)
	displayType := proxyType
	if plugin, ok := config["plugin"].(map[string]any); ok {
		if pluginType := stringValue(plugin, "type"); pluginType != "" {
			displayType = pluginType
		}
	}
	enabled, configured := config["enabled"].(bool)
	if !configured {
		enabled = true
	}
	view := proxyView{
		Instance:    instance,
		Name:        stringValue(proxy, "name"),
		Type:        proxyType,
		DisplayType: displayType,
		Local:       localEndpoint(config),
		Remote:      remoteEndpoint(config),
		Enabled:     enabled,
	}
	if !enabled {
		view.State, view.StateClass = "Disabled", "muted"
		return view
	}
	if statusUnavailable {
		view.State, view.StateClass = "Unknown", "muted"
		return view
	}
	if !foundStatus {
		view.State, view.StateClass = "Not running", "bad"
		return view
	}
	view.State = status.Status
	view.StateClass = "ok"
	view.Detail = status.Error
	if status.Error != "" {
		view.StateClass = "bad"
	}
	if status.RemoteAddr != "" {
		view.Remote = status.RemoteAddr
	}
	return view
}

func localEndpoint(proxy map[string]any) string {
	if local := endpoint(proxy, "localIP", "localPort"); local != "" {
		return local
	}
	if plugin, ok := proxy["plugin"].(map[string]any); ok {
		return stringValue(plugin, "localAddr")
	}
	return "—"
}

func stringValue(proxy map[string]any, key string) string {
	value, _ := proxy[key].(string)
	return value
}

func endpoint(proxy map[string]any, hostKey, portKey string) string {
	host := stringValue(proxy, hostKey)
	if port, ok := proxy[portKey]; ok {
		if host == "" {
			return fmt.Sprint(port)
		}
		return fmt.Sprintf("%s:%v", host, port)
	}
	return host
}

func remoteEndpoint(proxy map[string]any) string {
	if host := stringValue(proxy, "remoteIP"); host != "" {
		return endpoint(proxy, "remoteIP", "remotePort")
	}
	if port, ok := proxy["remotePort"]; ok {
		return fmt.Sprint(port)
	}
	if domains, ok := proxy["customDomains"].([]any); ok && len(domains) > 0 {
		values := make([]string, 0, len(domains))
		for _, domain := range domains {
			values = append(values, fmt.Sprint(domain))
		}
		return strings.Join(values, ", ")
	}
	if subdomain := stringValue(proxy, "subdomain"); subdomain != "" {
		return subdomain
	}
	return "—"
}

func (s *Server) render(w http.ResponseWriter, name string, page any) {
	var output bytes.Buffer
	if err := s.templates.ExecuteTemplate(&output, name, page); err != nil {
		log.Printf("render %q: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(output.Bytes())
}
func (s *Server) redirectMessage(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/?message="+url.QueryEscape(message), http.StatusSeeOther)
}
func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, err error) {
	http.Redirect(w, r, "/?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	for _, name := range units {
		size /= unit
		if size < unit || name == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", size, name)
		}
	}
	return fmt.Sprintf("%d B", value)
}
