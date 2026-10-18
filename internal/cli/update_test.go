package cli

import (
	"io"
	"strings"
	"testing"
)

func TestUpdateCommandRejectsFlagsAndArguments(t *testing.T) {
	for _, args := range [][]string{{"update", "--check"}, {"update", "v0.2.0"}, {"update", "--force"}} {
		if err := Run(t.Context(), args, io.Discard, "v0.1.0"); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	cmd := newUpdateCommand("dev")
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	if err := cmd.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("command did not run updater: %v", err)
	}
}
