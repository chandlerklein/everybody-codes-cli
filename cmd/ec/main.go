package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

const usage = "usage: ec init https://everybody.codes/event/<EVENT>/quests/<QUEST>"

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
	if len(args) != 2 || args[0] != "init" {
		return fmt.Errorf("%s", usage)
	}
	q, err := parseQuestURL(args[1])
	if err != nil {
		return err
	}
	return initQuest(ctx, q, output)
}
