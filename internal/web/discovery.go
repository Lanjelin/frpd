package web

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lanjelin/frpd/internal/frps"
)

type frpsDiscoveryPage struct {
	Servers []frpsDiscoveryServer
}

type frpsDiscoveryServer struct {
	Name    string
	Error   string
	Clients []frpsDiscoveredClient
}

type frpsDiscoveredClient struct {
	Name     string
	User     string
	Hostname string
	Online   bool
	Proxies  []frpsDiscoveredProxy
}

type frpsDiscoveredProxy struct {
	Name           string
	Type           string
	Status         string
	Remote         string
	Offline        bool
	TrafficIn      string
	TrafficOut     string
	TrafficTotal   int64
	Connections    int64
	VhostHTTPPort  int64
	VhostHTTPSPort int64
}

func (s *Server) frpsDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	page := frpsDiscoveryPage{Servers: make([]frpsDiscoveryServer, len(s.frpsServers))}
	names := make([]string, 0, len(s.frpsServers))
	for name := range s.frpsServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page.Servers[i] = s.discoverFRPS(ctx, name, s.frpsServers[name])
		}()
	}
	wg.Wait()
	s.render(w, "frps-discovery", page)
}

func (s *Server) discoverFRPS(ctx context.Context, name string, client *frps.Client) frpsDiscoveryServer {
	view := frpsDiscoveryServer{Name: name}
	var (
		clients    []frps.ClientInfo
		clientsErr error
		wg         sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		clients, clientsErr = client.Clients(ctx)
	}()
	proxies, proxiesErr := client.AllProxies(ctx)
	wg.Wait()
	if clientsErr != nil {
		view.Error = clientsErr.Error()
		return view
	}
	if proxiesErr != nil {
		view.Error = proxiesErr.Error()
		return view
	}

	configuredClientIDs := make(map[string]struct{})
	for _, instanceName := range s.names {
		if s.frpsByInstance[instanceName] == client {
			configuredClientIDs[s.clientIDs[instanceName]] = struct{}{}
		}
	}
	// Client IDs are scoped by user in FRPS. Without a configured user, only
	// exclude a client when the ID is unique on this server.
	clientIDCounts := make(map[string]int)
	for _, item := range clients {
		if _, ok := configuredClientIDs[item.ClientID]; ok {
			clientIDCounts[item.ClientID]++
		}
	}
	linked := make(map[string]bool)
	for _, item := range clients {
		if item.ClientID != "" && clientIDCounts[item.ClientID] == 1 {
			linked[item.User+"\x00"+item.ClientID] = true
		}
	}
	indexes := make(map[string]int)
	for _, item := range clients {
		identity := item.User + "\x00" + item.ClientID
		if item.ClientID != "" && linked[identity] {
			continue
		}
		label := item.ClientID
		if label == "" {
			label = item.Key
		}
		if label == "" {
			label = "Unknown client"
		}
		if item.ClientID != "" {
			indexes[identity] = len(view.Clients)
		}
		view.Clients = append(view.Clients, frpsDiscoveredClient{Name: label, User: item.User, Hostname: item.Hostname, Online: item.Online})
	}
	var serverInfo frps.ServerInfo
	for _, proxy := range proxies {
		if proxy.Conf.Subdomain != "" || ((proxy.Type == "http" || proxy.Type == "https") && len(proxy.Conf.CustomDomains) > 0) {
			serverInfo, _ = client.ServerInfo(ctx)
			break
		}
	}
	serverHost := s.frpsHosts[name]
	for _, proxy := range proxies {
		identity := proxy.User + "\x00" + proxy.ClientID
		if proxy.ClientID != "" && linked[identity] {
			continue
		}
		index, ok := indexes[identity]
		if !ok {
			label := proxy.ClientID
			if label == "" {
				label = "Unattributed proxies"
			}
			index = len(view.Clients)
			indexes[identity] = index
			view.Clients = append(view.Clients, frpsDiscoveredClient{Name: label, User: proxy.User})
		}
		view.Clients[index].Proxies = append(view.Clients[index].Proxies, frpsDiscoveredProxy{
			Name: proxy.Name, Type: proxy.Type, Status: proxy.Status, Remote: frpsRemote(proxy, serverInfo, serverHost), Offline: proxy.Status == "offline",
			TrafficIn: formatBytes(proxy.TodayTrafficIn), TrafficOut: formatBytes(proxy.TodayTrafficOut),
			TrafficTotal: proxy.TodayTrafficIn + proxy.TodayTrafficOut,
			Connections:  proxy.CurConns, VhostHTTPPort: serverInfo.VhostHTTPPort, VhostHTTPSPort: serverInfo.VhostHTTPSPort,
		})
	}
	for i := range view.Clients {
		sort.Slice(view.Clients[i].Proxies, func(a, b int) bool {
			return view.Clients[i].Proxies[a].Name < view.Clients[i].Proxies[b].Name
		})
	}
	sort.Slice(view.Clients, func(a, b int) bool {
		left, right := view.Clients[a], view.Clients[b]
		if left.User != right.User {
			return left.User < right.User
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	return view
}

func frpsRemote(proxy frps.ProxyMetric, serverInfo frps.ServerInfo, serverHost string) string {
	if proxy.Type == "tcp" || proxy.Type == "udp" {
		if proxy.Conf.RemotePort <= 0 {
			return "—"
		}
		port := strconv.Itoa(proxy.Conf.RemotePort)
		if serverHost != "" {
			return net.JoinHostPort(serverHost, port)
		}
		return port
	}
	var port int64
	switch proxy.Type {
	case "http":
		port = serverInfo.VhostHTTPPort
	case "https":
		port = serverInfo.VhostHTTPSPort
	}
	portSuffix := ""
	if port > 0 {
		portSuffix = ":" + strconv.FormatInt(port, 10)
	}
	remote := ""
	if portSuffix == "" {
		remote = strings.Join(proxy.Conf.CustomDomains, ", ")
	} else if len(proxy.Conf.CustomDomains) > 0 {
		var builder strings.Builder
		for i, domain := range proxy.Conf.CustomDomains {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(domain)
			builder.WriteString(portSuffix)
		}
		remote = builder.String()
	}
	if subdomain := proxy.Conf.Subdomain; subdomain != "" {
		if serverInfo.SubdomainHost != "" {
			subdomain += "." + strings.TrimPrefix(serverInfo.SubdomainHost, ".")
			subdomain += portSuffix
		} else {
			subdomain = "subdomain: " + subdomain
		}
		if remote != "" {
			remote += ", "
		}
		remote += subdomain
	}
	if remote == "" {
		return "—"
	}
	return remote
}

func (s *Server) pruneFRPS(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("server")
	client, ok := s.frpsServers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := client.PruneOffline(ctx); err != nil {
		s.redirectError(w, r, err)
		return
	}
	s.redirectMessage(w, r, "Cleared offline proxy statistics on FRPS "+name)
}
