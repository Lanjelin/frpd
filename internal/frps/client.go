// Package frps reads observability data from an frps management API.
package frps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/Lanjelin/frpd/internal/config"
)

type Client struct {
	baseURL, username, password string
	http                        *http.Client
}

type ServerInfo struct {
	Version         string           `json:"version"`
	VhostHTTPPort   int64            `json:"vhostHTTPPort"`
	VhostHTTPSPort  int64            `json:"vhostHTTPSPort"`
	SubdomainHost   string           `json:"subdomainHost"`
	TotalTrafficIn  int64            `json:"totalTrafficIn"`
	TotalTrafficOut int64            `json:"totalTrafficOut"`
	CurConns        int64            `json:"curConns"`
	ClientCounts    int64            `json:"clientCounts"`
	ProxyTypeCounts map[string]int64 `json:"proxyTypeCount"`
}

type ProxyMetric struct {
	Name string `json:"name"`
	Type string `json:"-"`
	User string `json:"user"`
	Conf struct {
		CustomDomains []string `json:"customDomains"`
		Subdomain     string   `json:"subdomain"`
		RemotePort    int      `json:"remotePort"`
	} `json:"conf"`
	ClientID        string `json:"clientID"`
	TodayTrafficIn  int64  `json:"todayTrafficIn"`
	TodayTrafficOut int64  `json:"todayTrafficOut"`
	CurConns        int64  `json:"curConns"`
	Status          string `json:"status"`
	LastStartTime   string `json:"lastStartTime"`
	LastCloseTime   string `json:"lastCloseTime"`
}

type ClientInfo struct {
	Key      string `json:"key"`
	User     string `json:"user"`
	ClientID string `json:"clientID"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
}

func New(instance config.FRPSServer, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(instance.URL, "/"), username: instance.Username, password: instance.Password, http: httpClient}
}

func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	var info ServerInfo
	err := c.get(ctx, "/api/serverinfo", &info)
	return info, err
}

func (c *Client) ProxiesByType(ctx context.Context, proxyType string) ([]ProxyMetric, error) {
	var response struct {
		Proxies []ProxyMetric `json:"proxies"`
	}
	if err := c.get(ctx, "/api/proxy/"+url.PathEscape(proxyType), &response); err != nil {
		return nil, err
	}
	return response.Proxies, nil
}

func (c *Client) Clients(ctx context.Context) ([]ClientInfo, error) {
	var clients []ClientInfo
	err := c.get(ctx, "/api/clients", &clients)
	return clients, err
}

// proxyTypes are the frps proxy types AllProxies queries, in result order.
var proxyTypes = [...]string{"tcp", "udp", "http", "https", "tcpmux", "stcp", "xtcp", "sudp"}

// AllProxies queries every proxy type concurrently. Results keep proxyTypes
// order, and the error of the earliest failing type is returned.
func (c *Client) AllProxies(ctx context.Context) ([]ProxyMetric, error) {
	results := make([][]ProxyMetric, len(proxyTypes))
	errs := make([]error, len(proxyTypes))
	var wg sync.WaitGroup
	for i, proxyType := range proxyTypes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = c.ProxiesByType(ctx, proxyType)
		}()
	}
	wg.Wait()
	var proxies []ProxyMetric
	for i, proxyType := range proxyTypes {
		if errs[i] != nil {
			return nil, errs[i]
		}
		for j := range results[i] {
			results[i][j].Type = proxyType
		}
		proxies = append(proxies, results[i]...)
	}
	return proxies, nil
}

func (c *Client) PruneOffline(ctx context.Context) error {
	return c.request(ctx, http.MethodDelete, "/api/proxies?status=offline", nil)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.request(ctx, http.MethodGet, path, out)
}

func (c *Client) request(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build FRPS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("FRPS API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			return fmt.Errorf("FRPS API %s %s: %s", method, path, resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 256))
		if err != nil {
			return fmt.Errorf("read FRPS API response: %w", err)
		}
		detail, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		if detail == "" || strings.HasPrefix(detail, "<") {
			return fmt.Errorf("FRPS API %s %s: %s", method, path, resp.Status)
		}
		return fmt.Errorf("FRPS API %s %s: %s: %s", method, path, resp.Status, detail)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read FRPS API response: %w", err)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode FRPS API response: %w", err)
		}
	}
	return nil
}
