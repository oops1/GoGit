package app

import (
	"context"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/console"
)

var newConsoleView = console.NewView

var runConsoleLine = console.Run

func (a *App) registerConsoleHandlers() {
	a.handlers[CmdConsole] = a.openConsole
}

func (a *App) openConsole() {
	view, err := newConsoleView()
	if err != nil {
		a.log.Warn("open console dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.ConsoleFailed", err))
		return
	}
	view.OnSubmit = func(line string) { a.runConsoleCommand(view, line) }
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) runConsoleCommand(view *console.View, line string) {
	if a.opened() == nil {
		view.Finish("", console.ErrNoRepository)
		return
	}
	var out string
	started := a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		text, err := runConsoleLine(ctx, console.Env{Repo: r, Transport: a.transportOptions}, line)
		out = text
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("console command failed", "command", line, "error", err)
		}
		view.Finish(out, err)
		a.refreshAtOnce()
	})
	if !started {
		view.Finish("", console.ErrBusy)
	}
}
