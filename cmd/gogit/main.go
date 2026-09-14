package main

import (
	"fmt"
	"os"
	"time"

	"github.com/oops1/gogit/internal/app"
	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/logx"
	"github.com/oops1/gogit/internal/winconsole"
)

var (
	showStartupMessage = nativeStartupMessage
	startupClock       = time.Now
)

func main() {
	if err := run(); err != nil {
		reportStartupFailure(err)
		os.Exit(1)
	}
}

func run() error {
	winconsole.Hide()
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, backup, err := config.LoadOrRecover(paths.ConfigFile(), startupClock())
	if err != nil {
		return err
	}
	logger, err := logx.Open(paths.LogFile(), logx.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "logx:", err)
		logger = logx.Discard()
	}
	defer logger.Close()
	if backup != "" {
		logger.Slog().Warn("config file could not be read and was moved aside", "backup", backup)
		localizeStartup(paths.UserI18NDir(), cfg.Language)
		showStartupMessage(i18n.T("Startup.Title"), i18n.Tf("Startup.ConfigRecovered", backup))
	}
	a, err := app.New(cfg, paths, logger.Slog())
	if err != nil {
		return err
	}
	return a.Run()
}

func localizeStartup(userDir, language string) {
	if _, err := i18n.Install(userDir); err != nil {
		_, _ = i18n.Install("")
	}
	i18n.Apply(language)
}

func reportStartupFailure(err error) {
	text := logx.RedactText(err.Error())
	fmt.Fprintln(os.Stderr, text)
	_, _ = i18n.Install("")
	showStartupMessage(i18n.T("Startup.Title"), i18n.Tf("Startup.Failed", text))
}
