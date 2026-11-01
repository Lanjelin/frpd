package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNormalizesInstanceURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpd.toml")
	if err := os.WriteFile(path, []byte("listen = \":9090\"\n[[frpc]]\nname = \"edge\"\nurl = \"http://frpc:7400/\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" || cfg.Instances[0].URL != "http://frpc:7400" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestLoadRejectsDuplicateNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpd.toml")
	content := "[[frpc]]\nname = \"edge\"\nurl = \"http://one\"\n[[frpc]]\nname = \"edge\"\nurl = \"http://two\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted duplicate names")
	}
}

func TestLoadNormalizesAndValidatesAllowedOrigins(t *testing.T) {
	load := func(origins string) (Config, error) {
		path := filepath.Join(t.TempDir(), "frpd.toml")
		if err := os.WriteFile(path, []byte("allowedOrigins = "+origins+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return Load(path)
	}
	cfg, err := load(`["HTTPS://Frp.Example.com:443/", "http://nas.lan:8080", "http://[::1]:80", "http://frp.lan:80"]`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://frp.example.com", "http://nas.lan:8080", "http://[::1]", "http://frp.lan"}
	if strings.Join(cfg.AllowedOrigins, " ") != strings.Join(want, " ") {
		t.Fatalf("allowedOrigins = %q, want %q", cfg.AllowedOrigins, want)
	}
	for _, invalid := range []string{`["*"]`, `["frp.lan"]`, `["ftp://frp.lan"]`, `["https://frp.lan/path"]`, `["https://user@frp.lan"]`, `["null"]`, `[""]`} {
		if _, err := load(invalid); err == nil || !strings.Contains(err.Error(), "allowedOrigins[0]") {
			t.Fatalf("allowedOrigins %s error = %v", invalid, err)
		}
	}
}

func TestLoadRequiresClientIDForFRPSLinkedInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpd.toml")
	content := "[[frps]]\nname = \"public\"\nurl = \"http://frps:7500\"\n[[frpc]]\nname = \"Display name\"\nurl = \"http://frpc:7400\"\nfrps = \"public\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "requires clientID") {
		t.Fatalf("missing FRPS clientID error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content+"clientID = \" actual-id \"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Instances[0].Name != "Display name" || cfg.Instances[0].ClientID != "actual-id" {
		t.Fatalf("instance identity = %+v", cfg.Instances[0])
	}
}

func TestLoadRejectsOldInstanceTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpd.toml")
	content := "[[instances]]\nname = \"edge\"\nurl = \"http://frpc:7400\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "instances") {
		t.Fatalf("old table error = %v", err)
	}
}

func TestLoadRequiresPairedDashboardCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpd.toml")
	for _, content := range []string{
		"username = \"admin\"\n",
		"password = \"secret\"\n",
		"username = \"admin\"\npassword = \"\"\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "username and password") {
			t.Fatalf("config %q error = %v", content, err)
		}
	}
	if err := os.WriteFile(path, []byte("username = \"admin\"\npassword = \"secret\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Username != "admin" || cfg.Password != "secret" {
		t.Fatalf("credentials = %q, %q; error = %v", cfg.Username, cfg.Password, err)
	}
}

func TestDefaultPathUsesFRPDConfig(t *testing.T) {
	t.Setenv("FRPD_CONFIG", "")
	if path := DefaultPath(); path != "frpd.toml" {
		t.Fatalf("DefaultPath() = %q", path)
	}

	t.Setenv("FRPD_CONFIG", "/etc/frpd.toml")
	if path := DefaultPath(); path != "/etc/frpd.toml" {
		t.Fatalf("DefaultPath() with environment override = %q", path)
	}
}
