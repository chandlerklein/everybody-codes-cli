package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })
	for _, tt := range []struct {
		name    string
		version string
		info    *debug.BuildInfo
		want    string
	}{
		{"release", "v0.1.0", nil, "v0.1.0"},
		{"prerelease", "v0.2.0-rc.1", nil, "v0.2.0-rc.1"},
		{"linker takes precedence", "v0.2.0", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.2.0"},
		{"go install", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.1.0"},
		{"local build", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{"empty build info", "dev", &debug.BuildInfo{}, "dev"},
		{"missing build info", "dev", nil, "dev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			version = tt.version
			if got := buildVersion(tt.info); got != tt.want {
				t.Fatalf("version = %q, want %q", got, tt.want)
			}
		})
	}
}
