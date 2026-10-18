package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestRunStatusWithDirectory(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "2024/quest-01/part-02/input.txt"), "decoded")
	for _, args := range [][]string{
		{"status", "--dir", base},
		{"--dir", base, "status"},
	} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err != nil {
			t.Fatal(err)
		}
		if got, want := output.String(), "2024\n  quest-01\n    part-01: locked\n    part-02: ready\n    part-03: locked\n"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}
