// Package config loads frpd's own TOML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Listen         string       `toml:"listen"`
	Username       string       `toml:"username"`
	Password       string       `toml:"password"`
	AllowedOrigins []string     `toml:"allowedOrigins"`
	FRPSServers    []FRPSServer `toml:"frps"`
	Instances      []Instance   `toml:"frpc"`
}

// FRPSServer is optional observability-only access to an frps management API.
type FRPSServer struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type Instance struct {
	Name     string `toml:"name"`
	Order    *int   `toml:"order"`
	ClientID string `toml:"clientID"`
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	FRPS     string `toml:"frps"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}

	var cfg Config
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg); err != nil {
		var unknown *toml.StrictMissingError
		if errors.As(err, &unknown) {
			return Config{}, fmt.Errorf("parse TOML configuration: %s", unknown.String())
		}
		return Config{}, fmt.Errorf("parse TOML configuration: %w", err)
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return Config{}, fmt.Errorf("dashboard username and password must both be set")
	}
	for i, origin := range cfg.AllowedOrigins {
		normalized, err := NormalizeOrigin(origin)
		if err != nil {
			return Config{}, fmt.Errorf("allowedOrigins[%d]: %w", i, err)
		}
		cfg.AllowedOrigins[i] = normalized
	}
	frpsNames := make(map[string]struct{}, len(cfg.FRPSServers))
	for i := range cfg.FRPSServers {
		server := &cfg.FRPSServers[i]
		server.Name = strings.TrimSpace(server.Name)
		server.URL = strings.TrimRight(strings.TrimSpace(server.URL), "/")
		if server.Name == "" || server.URL == "" {
			return Config{}, fmt.Errorf("frps[%d] requires name and url", i)
		}
		if _, exists := frpsNames[server.Name]; exists {
			return Config{}, fmt.Errorf("duplicate frps name %q", server.Name)
		}
		frpsNames[server.Name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(cfg.Instances))
	for i := range cfg.Instances {
		instance := &cfg.Instances[i]
		instance.Name = strings.TrimSpace(instance.Name)
		instance.URL = strings.TrimRight(strings.TrimSpace(instance.URL), "/")
		instance.ClientID = strings.TrimSpace(instance.ClientID)
		instance.FRPS = strings.TrimSpace(instance.FRPS)
		if instance.Name == "" || instance.URL == "" {
			return Config{}, fmt.Errorf("frpc[%d] requires name and url", i)
		}
		if instance.FRPS != "" {
			if instance.ClientID == "" {
				return Config{}, fmt.Errorf("frpc[%d] requires clientID when frps is set", i)
			}
			if _, exists := frpsNames[instance.FRPS]; !exists {
				return Config{}, fmt.Errorf("frpc[%d] references unknown frps %q", i, instance.FRPS)
			}
		}
		if _, exists := seen[instance.Name]; exists {
			return Config{}, fmt.Errorf("duplicate instance name %q", instance.Name)
		}
		seen[instance.Name] = struct{}{}
	}
	return cfg, nil
}

// NormalizeOrigin reduces an origin such as "HTTPS://Frp.Example.com:443/" to
// the form browsers send in the Origin header ("https://frp.example.com").
// Only scheme, host and optional port are accepted.
func NormalizeOrigin(value string) (string, error) {
	invalid := fmt.Errorf("%q must be an origin like https://host[:port] (scheme and host only, no wildcards)", value)
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", invalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", invalid
	}
	host, port := strings.ToLower(parsed.Hostname()), parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}

func DefaultPath() string {
	if path := os.Getenv("FRPD_CONFIG"); path != "" {
		return path
	}
	return filepath.Join(".", "frpd.toml")
}
