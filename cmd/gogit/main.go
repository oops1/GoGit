package main

import (
	"fmt"
	"os"

	"github.com/oops1/gogit/internal/app"
	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/logx"
	"github.com/oops1/gogit/internal/winconsole"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	winconsole.Hide()
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return err
	}
	logger, err := logx.Open(paths.LogFile(), logx.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "logx:", err)
		logger = logx.Discard()
	}
	defer logger.Close()
	a, err := app.New(cfg, paths, logger.Slog())
	if err != nil {
		return err
	}
	return a.Run()
}
