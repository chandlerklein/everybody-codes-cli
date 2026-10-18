package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

func printVersion(output io.Writer) error {
	info, _ := debug.ReadBuildInfo()
	_, err := fmt.Fprintln(output, "ec", buildVersion(info))
	return err
}

func buildVersion(info *debug.BuildInfo) string {
	if info == nil {
		return "devel"
	}
	if version := info.Main.Version; version != "" && version != "(devel)" {
		return version
	}
	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "devel"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return "devel (" + revision + ")"
}
