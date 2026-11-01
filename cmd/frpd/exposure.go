package main

import (
	"log"
	"net"
)

// exposedWithoutAuth reports whether the dashboard can be reached from other
// hosts while Basic authentication is off.
func exposedWithoutAuth(username string, addr net.Addr) bool {
	if username != "" {
		return false
	}
	tcp, ok := addr.(*net.TCPAddr)
	return !ok || !tcp.IP.IsLoopback()
}

func warnIfExposed(username string, addr net.Addr) {
	if exposedWithoutAuth(username, addr) {
		log.Printf("WARNING: dashboard on %s has no authentication; anyone who can connect can create, change and delete proxies. Set username and password, listen on 127.0.0.1, or put it behind an authenticated reverse proxy", addr)
	}
}
