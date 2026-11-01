// Package store speaks to frpc's authenticated Store API.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Lanjelin/frpd/internal/config"
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

type Proxy map[string]any

// ProxyStatus is frpc's live state for one configured proxy.
type ProxyStatus struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Error      string `json:"err"`
	LocalAddr  string `json:"local_addr"`
	Plugin     string `json:"plugin"`
	RemoteAddr string `json:"remote_addr"`
	Source     string `json:"source"`
}

func New(instance config.Instance, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(instance.URL, "/"), username: instance.Username, password: instance.Password, http: httpClient}
}

func (c *Client) ListProxies(ctx context.Context) ([]Proxy, error) {
	var envelope struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/store/proxies", nil, &envelope); err != nil {
		return nil, err
	}
	return envelope.Proxies, nil
}

// ProxyStatuses returns runtime state keyed by proxy name.
func (c *Client) ProxyStatuses(ctx context.Context) (map[string]ProxyStatus, error) {
	var response map[string][]ProxyStatus
	if err := c.do(ctx, http.MethodGet, "/api/status", nil, &response); err != nil {
		return nil, err
	}
	statuses := make(map[string]ProxyStatus)
	for _, byType := range response {
		for _, status := range byType {
			statuses[status.Name] = status
		}
	}
	return statuses, nil
}

func (c *Client) GetProxy(ctx context.Context, name string) (Proxy, error) {
	var proxy Proxy
	err := c.do(ctx, http.MethodGet, "/api/store/proxies/"+url.PathEscape(name), nil, &proxy)
	return proxy, err
}

func (c *Client) CreateProxy(ctx context.Context, proxy Proxy) error {
	return c.do(ctx, http.MethodPost, "/api/store/proxies", proxy, nil)
}

func (c *Client) UpdateProxy(ctx context.Context, name string, proxy Proxy) error {
	return c.do(ctx, http.MethodPut, "/api/store/proxies/"+url.PathEscape(name), proxy, nil)
}

func (c *Client) DeleteProxy(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/store/proxies/"+url.PathEscape(name), nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Store API request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read Store API response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Store API %s %s: %s", method, path, errorDetail(data, resp.Status))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode Store API response: %w", err)
		}
	}
	return nil
}

const maxErrorDetail = 256

// errorDetail reduces an upstream error body to its first line, capped in
// length, because it ends up in redirect URLs and page text. HTML or empty
// bodies fall back to the HTTP status.
func errorDetail(body []byte, status string) string {
	detail, _, _ := strings.Cut(strings.TrimSpace(string(body)), "\n")
	detail = strings.TrimSpace(detail)
	if detail == "" || strings.HasPrefix(detail, "<") {
		return status
	}
	if len(detail) > maxErrorDetail {
		detail = strings.ToValidUTF8(detail[:maxErrorDetail], "") + "…"
	}
	return detail
}
