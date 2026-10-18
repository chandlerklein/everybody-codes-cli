package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	for _, test := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{name: "missing build information", want: "devel"},
		{name: "development without VCS", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, want: "devel"},
		{name: "installed release", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, want: "v1.2.3"},
		{name: "installed revision", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261017000000-123456789abc"}}, want: "v0.0.0-20261017000000-123456789abc"},
		{name: "development revision", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "123456789abcdef"}}}, want: "devel (123456789abc)"},
		{name: "modified development revision", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, want: "devel (123456789abc-dirty)"},
		{name: "short revision", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "1234567"}}}, want: "devel (1234567)"},
		{name: "installed version takes precedence", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, want: "v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVersion(test.info); got != test.want {
				t.Fatalf("version = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunVersionOffline(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("EVERYBODY_CODES_SESSION_COOKIE", "")
	oldClient := apiClient
	t.Cleanup(func() { apiClient = oldClient })
	apiClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("version command made an HTTP request")
		return nil, errors.New("offline")
	})}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--version"}, &output); err != nil {
		t.Fatal(err)
	}
	info, _ := debug.ReadBuildInfo()
	if got, want := output.String(), "ec "+buildVersion(info)+"\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version command created files: entries = %v, error = %v", entries, err)
	}
}

type versionErrorWriter struct{ err error }

func (w versionErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestPrintVersionPropagatesWriteError(t *testing.T) {
	want := errors.New("write failed")
	if err := printVersion(versionErrorWriter{want}); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestRunVersionRejectsAdditionalArguments(t *testing.T) {
	for _, args := range [][]string{{"--version", "extra"}, {"--version", "--dir", "elsewhere"}} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), "unknown option: --version") {
			t.Fatalf("run(%v) = %v, want unknown version option", args, err)
		}
	}
}
