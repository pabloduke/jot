package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/paws-in-the-machine/jot/internal/jot"
)

func main() {
	// A cancellable context lets long-running work (serve, git operations)
	// shut down cleanly on Ctrl-C instead of being killed mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := jot.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "jot:", err)
		os.Exit(jot.ExitCode(err))
	}
}
