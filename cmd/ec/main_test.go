package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestCommandHelp(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "ec [<quest-url> | --event <event> --quest <quest>]"},
		{[]string{"-h"}, "ec [<quest-url>"},
		{[]string{"--dir", "elsewhere", "--help"}, "ec [<quest-url>"},
		{[]string{testURL, "-h"}, "ec [<quest-url>"},
		{[]string{"-e", "2024", "-q", "01", "--help"}, "ec -e 2024 -q 01"},
		{[]string{"auth", "--help"}, "ec auth [flags]"},
		{[]string{"auth", "check", "--help"}, "ec auth check [flags]"},
		{[]string{"status", "--help"}, "ec status [flags]"},
		{[]string{"help", "auth", "check"}, "ec auth check [flags]"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var output bytes.Buffer
			if err := run(t.Context(), tt.args, &output); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), tt.want) {
				t.Fatalf("help = %q, want %q", output.String(), tt.want)
			}
			if strings.Contains(tt.want, "quest-url") {
				for _, flag := range []string{"-e, --event", "-q, --quest", "leading zeros"} {
					if !strings.Contains(output.String(), flag) {
						t.Fatalf("help missing %q: %s", flag, output.String())
					}
				}
			}
			if tt.args[0] == "auth" && strings.Contains(output.String(), "--dir") {
				t.Fatal("auth help includes the download/status directory flag")
			}
			if tt.args[0] == "auth" || tt.args[0] == "status" {
				if strings.Contains(output.String(), "--event") || strings.Contains(output.String(), "--quest") {
					t.Fatal("subcommand help includes quest selection flags")
				}
			}
			if strings.Contains(tt.want, "quest-url") && !strings.Contains(output.String(), "--version") {
				t.Fatal("root help missing the version flag")
			}
		})
	}
}

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

func TestCommandVersionWithoutSideEffects(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })
	for _, value := range []string{"dev", "v0.1.0", "v0.2.0-rc.1"} {
		t.Run(value, func(t *testing.T) {
			api := setup(t)
			version = value
			// Version reporting works without a session or quest arguments.
			if err := os.Remove(filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session")); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := run(t.Context(), []string{"--version"}, &output); err != nil {
				t.Fatal(err)
			}
			if got, want := output.String(), "ec "+value+"\n"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
			if len(api.calls) != 0 {
				t.Fatal("version command made HTTP requests")
			}
			assertMissing(t, "2024")
			assertMissing(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"))
		})
	}
}

func TestCommandQuestFlags(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		number int
	}{
		{[]string{"-e", "2024", "-q", "1"}, 1},
		{[]string{"--event", "2024", "--quest", "01"}, 1},
		{[]string{"-q", "08", "-e", "2024"}, 8},
		{[]string{"--event=2024", "--quest=09"}, 9},
		{[]string{"-e2024", "-q00010"}, 10},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			api := setup(t)
			keysEndpoint := fmt.Sprintf("%s/event/2024/quest/%d", apiBaseURL, tt.number)
			inputsEndpoint := fmt.Sprintf("https://everybody.codes/assets/2024/%d/input/8.json", tt.number)
			api.bodies[keysEndpoint] = api.bodies[keysURL]
			api.bodies[inputsEndpoint] = api.bodies[inputsURL]
			var output bytes.Buffer
			args := append(slices.Clone(tt.args), "--dir", "solutions")
			if err := run(t.Context(), args, &output); err != nil {
				t.Fatal(err)
			}
			for part := 1; part <= 3; part++ {
				dir := fmt.Sprintf("solutions/2024/quest-%02d/part-%02d/input", tt.number, part)
				assertFile(t, filepath.Join(dir, "input-encoded.txt"), testNotes[part-1].encoded)
				assertFile(t, filepath.Join(dir, "input.txt"), testNotes[part-1].plain)
			}
			wantCalls := []string{userURL, inputsEndpoint, keysEndpoint}
			slices.Sort(api.calls)
			slices.Sort(wantCalls)
			if !slices.Equal(api.calls, wantCalls) {
				t.Fatalf("requests = %v, want %v", api.calls, wantCalls)
			}
			// The equivalent URL reuses the same files and needs no more requests.
			api.calls = nil
			url := fmt.Sprintf("https://everybody.codes/event/2024/quests/%d", tt.number)
			if err := run(t.Context(), []string{url, "--dir", "solutions"}, io.Discard); err != nil {
				t.Fatal(err)
			}
			if len(api.calls) != 0 {
				t.Fatal("URL did not reuse the shorthand's downloaded inputs")
			}
		})
	}
}

func TestCommandRejectsInvalidQuestFlagsWithoutSideEffects(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"-e", "2024"}, "--event and --quest must be supplied together"},
		{[]string{"-q", "1"}, "--event and --quest must be supplied together"},
		{[]string{testURL, "-e", "2024", "-q", "1"}, "use either a quest URL"},
		{[]string{"-e", "", "-q", "1"}, "event must be"},
		{[]string{"-e", "../2024", "-q", "1"}, "event must be"},
		{[]string{"-e", "2024/quests/1", "-q", "1"}, "event must be"},
		{[]string{"-e", "2024?foo", "-q", "1"}, "event must be"},
		{[]string{"-e", "2024", "-q", ""}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "0"}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "-1"}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "one"}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "1.5"}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "0x01"}, "quest number must be"},
		{[]string{"-e", "2024", "-q", "999999999999999999999"}, "quest number must be"},
		{[]string{"-e", "2024", "-q"}, "flag needs an argument"},
		{[]string{"-e", "2024", "-q", "1", "--dir="}, "--dir requires a nonempty directory"},
		{[]string{"auth", "-e", "2024", "-q", "1"}, "unsupported auth flags"},
		{[]string{"status", "-e", "2024", "-q", "1"}, "unknown shorthand flag"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			api := setup(t)
			var output bytes.Buffer
			err := run(t.Context(), tt.args, &output)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if len(api.calls) != 0 || output.Len() != 0 {
				t.Fatalf("invalid arguments caused requests %v or output %q", api.calls, output.String())
			}
			assertMissing(t, "2024")
		})
	}
}

func TestCommandRejectsInvalidArgumentsWithoutSideEffects(t *testing.T) {
	for _, args := range [][]string{
		{"unknown", testURL},
		{testURL, "extra"},
		{"init", testURL},
		{"status", "extra"},
		{"status", "--dir="},
		{"auth", testSession},
		{"auth", "check", testSession},
		{"auth", "--" + testSession},
		{"auth", "-" + testSession},
		{"auth", "--help=" + testSession},
		{"auth", "check", "--" + testSession},
		{"auth", "check", "-" + testSession},
		{"auth", "check", "--help=" + testSession},
		{"auth", "--stdin"},
		{"auth", "--dir", "elsewhere"},
		{"--dir", "elsewhere", "auth"},
		{"auth", "check", "--dir", "elsewhere"},
		{"--dir", "elsewhere", "auth", "check"},
		{"version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			api := setup(t)
			var output bytes.Buffer
			err := run(t.Context(), args, &output)
			if err == nil {
				t.Fatal("accepted invalid arguments")
			}
			if strings.Contains(err.Error(), testSession) {
				t.Fatal("argument error exposed the cookie")
			}
			if output.Len() != 0 {
				t.Fatalf("printed output for invalid arguments: %q", output.String())
			}
			if len(api.calls) != 0 {
				t.Fatal("invalid arguments made HTTP requests")
			}
			assertFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), testSession+"\n")
			assertMissing(t, "2024")
		})
	}
}

func TestAuthCommandUsesInjectedInput(t *testing.T) {
	setup(t)
	cmd := newRootCommand()
	cmd.SetArgs([]string{"auth"})
	cmd.SetIn(strings.NewReader("replacement-cookie\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), "replacement-cookie\n")
	if !strings.Contains(output.String(), "Session saved") || strings.Contains(output.String(), "replacement-cookie") {
		t.Fatalf("auth output = %q", output.String())
	}
}

func TestAuthCommandCancellation(t *testing.T) {
	setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd := newRootCommand()
	cmd.SetArgs([]string{"auth"})
	cmd.SetIn(strings.NewReader("replacement-cookie\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	if output.Len() != 0 {
		t.Fatalf("canceled command printed output: %q", output.String())
	}
	assertFile(t, filepath.Join(os.Getenv("HOME"), ".config", "everybody-codes", "session"), testSession+"\n")
}

func TestCommandsDoNotShareFlagValues(t *testing.T) {
	setup(t)
	writeTestFile(t, "2024/quest-01/part-01/input/input.txt", "decoded")
	var output bytes.Buffer
	if err := run(t.Context(), []string{"status", "--dir", t.TempDir()}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "2024\n  quest-01\n    part-01: ready") {
		t.Fatalf("status reused previous command state: %q", output.String())
	}
}

func TestRunWithoutArgumentsShowsHelp(t *testing.T) {
	var help bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, &help); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {}} {
		var output bytes.Buffer
		if err := run(t.Context(), args, &output); err != nil {
			t.Fatal(err)
		}
		if output.String() != help.String() {
			t.Fatalf("output = %q, want help %q", output.String(), help.String())
		}
	}
}
