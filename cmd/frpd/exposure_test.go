package main

import (
	"net"
	"testing"
)

func TestExposedWithoutAuthOnlyWarnsForReachableUnauthenticatedListeners(t *testing.T) {
	tests := []struct {
		name     string
		username string
		addr     string
		want     bool
	}{
		{"all interfaces, no auth", "", "0.0.0.0:8080", true},
		{"all interfaces ipv6, no auth", "", "[::]:8080", true},
		{"lan address, no auth", "", "192.168.1.5:8080", true},
		{"loopback ipv4, no auth", "", "127.0.0.1:8080", false},
		{"loopback ipv6, no auth", "", "[::1]:8080", false},
		{"all interfaces with auth", "admin", "0.0.0.0:8080", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addr, err := net.ResolveTCPAddr("tcp", test.addr)
			if err != nil {
				t.Fatal(err)
			}
			if got := exposedWithoutAuth(test.username, addr); got != test.want {
				t.Fatalf("exposedWithoutAuth(%q, %s) = %v, want %v", test.username, test.addr, got, test.want)
			}
		})
	}
}
