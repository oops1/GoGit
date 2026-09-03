package main

import (
	"fmt"
	"os"

	"github.com/oops1/gogit/internal/app"
	"github.com/oops1/gogit/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return err
	}
	a, err := app.New(cfg, paths)
	if err != nil {
		return err
	}
	return a.Run()
}
