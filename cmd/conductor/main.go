package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/conductor-sh/conductor/cmd/conductor/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cmd.NewRootCommand()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return
	}
	// Subcommands that print their own categorized stderr return
	// cmd.ErrSilent so we skip the default duplicated error line and
	// just exit non-zero.
	if !errors.Is(err, cmd.ErrSilent) {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}
