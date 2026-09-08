package app

import (
	"os/exec"
	"path/filepath"

	"github.com/oops1/headless-gui/v3/widget"
)

var setClipboard = widget.ClipboardSetText

var startCommand = (*exec.Cmd).Start

func (a *App) copyToClipboard(text string) {
	if text == "" {
		return
	}
	setClipboard(text)
}

func (a *App) revealPath(path string) {
	if path == "" {
		return
	}
	a.runTool(revealCommand(path), "reveal path failed", path)
}

func (a *App) openTerminalAt(path string) {
	if path == "" {
		return
	}
	a.runTool(terminalCommand(path), "open terminal failed", path)
}

func (a *App) runTool(cmd *exec.Cmd, message, path string) {
	if err := startCommand(cmd); err != nil {
		a.log.Warn(message, "path", path, "error", err)
	}
}

func containingDirectory(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}
