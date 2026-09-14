package app

import (
	"context"
	"os"
	"path/filepath"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

var removePath = os.RemoveAll

func (a *App) openFile(path string) {
	a.runTool(openURLCommand(path), "open file failed", path)
}

func (a *App) deleteFile(path string) {
	title := i18n.T("Dialog.DeleteFile.Title")
	message := i18n.Tf("Dialog.DeleteFile.Message", filepath.Base(path))
	a.askConfirm(title, message, func(ok bool) {
		if !ok {
			return
		}
		a.startWrite(func(context.Context, *gitrepo.Repository) error {
			return removePath(path)
		}, func(err error) {
			if err != nil {
				a.log.Warn("delete file failed", "path", path, "error", err)
				a.statusLabel.SetText(i18n.Tf("Status.DeleteFailed", err))
				return
			}
			a.clearFilesSelection()
		})
	})
}
