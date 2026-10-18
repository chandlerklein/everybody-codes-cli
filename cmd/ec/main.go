package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

const usage = "usage: ec init [--dir <directory>] https://everybody.codes/event/<EVENT>/quests/<QUEST>"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ec:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(output, usage)
		return err
	}
	if len(args) == 0 || args[0] != "init" {
		return fmt.Errorf("%s", usage)
	}
	dir := "."
	var positional []string
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--dir requires a directory\n%s", usage)
			}
			dir = args[i]
		case strings.HasPrefix(arg, "--dir="):
			dir = strings.TrimPrefix(arg, "--dir=")
		case arg == "--help" || arg == "-h":
			_, err := fmt.Fprintln(output, usage)
			return err
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown option: %s\n%s", arg, usage)
		default:
			positional = append(positional, arg)
		}
	}
	if dir == "" {
		return fmt.Errorf("--dir requires a nonempty directory\n%s", usage)
	}
	if len(positional) != 1 {
		return fmt.Errorf("%s", usage)
	}
	q, err := parseQuestURL(positional[0])
	if err != nil {
		return err
	}
	return initQuest(ctx, q, dir, output)
}
