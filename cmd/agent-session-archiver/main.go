package main

import (
	"context"
	"fmt"
	"github.com/prime-radiant-inc/agent-session-archiver/internal/archive"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) == 2 && os.Args[1] == "_helper" {
		if e := archive.Helper(ctx, os.Stdin, os.Stdout); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	os.Exit(archive.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
