package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/chandlerklein/everybody-codes-cli/internal/cli"
)

// GoReleaser sets version to the release tag with -ldflags.
var version = "dev"

func buildVersion(info *debug.BuildInfo) string {
	if version != "dev" {
		return version
	}
	// go install module@version embeds the module version without linker flags.
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	info, _ := debug.ReadBuildInfo()
	if err := cli.Run(ctx, os.Args[1:], os.Stdout, buildVersion(info)); err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		os.Exit(1)
	}
}
