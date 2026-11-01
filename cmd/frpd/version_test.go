package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionPrefersInjectedThenModuleThenDev(t *testing.T) {
	module := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: version}}
	}
	tests := []struct {
		name     string
		injected string
		info     *debug.BuildInfo
		want     string
	}{
		{"injected wins over module version", "v1.2.3", module("v9.9.9"), "v1.2.3"},
		{"injected describe string", "v1.2.3-4-gabcdef-dirty", nil, "v1.2.3-4-gabcdef-dirty"},
		{"default uses module version", "dev", module("v0.4.0"), "v0.4.0"},
		{"empty uses module version", "", module("v0.4.0"), "v0.4.0"},
		{"source build reports dev", "dev", module("(devel)"), "dev"},
		{"missing build info reports dev", "dev", nil, "dev"},
		{"empty module version reports dev", "dev", module(""), "dev"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveVersion(test.injected, test.info); got != test.want {
				t.Fatalf("resolveVersion(%q) = %q, want %q", test.injected, got, test.want)
			}
		})
	}
}
