package main

import "runtime/debug"

// version is set at build time: -ldflags "-X main.version=v1.2.3".
// An unset value falls back to module build info (go install ...@v1.2.3).
var version = "dev"

func buildVersion() string {
	info, _ := debug.ReadBuildInfo()
	return resolveVersion(version, info)
}

func resolveVersion(injected string, info *debug.BuildInfo) string {
	if injected != "" && injected != "dev" {
		return injected
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
