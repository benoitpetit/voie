package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/cli"
	"github.com/benoitpetit/voie/internal/runtime"
	"github.com/benoitpetit/voie/utils"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	utils.SetNoColor(true)
	utils.SetOutput(os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Execute(ctx, args, os.Stdin, os.Stdout, os.Stderr, func() (cli.Runtime, error) {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		if cfg.EnableDebug {
			utils.SetLevel(utils.DEBUG)
		}
		return runtime.New(cfg)
	})
}
