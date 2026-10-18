package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandHelp(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "ec <quest-url>"},
		{[]string{"-h"}, "ec <quest-url>"},
		{[]string{"--dir", "elsewhere", "--help"}, "ec <quest-url>"},
		{[]string{testURL, "-h"}, "ec <quest-url>"},
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
			if tt.args[0] == "auth" && strings.Contains(output.String(), "--dir") {
				t.Fatal("auth help includes the download/status directory flag")
			}
			if strings.Contains(output.String(), "--version") {
				t.Fatal("help includes a version flag")
			}
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
		{"--version"},
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
