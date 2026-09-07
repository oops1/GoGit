package app

import (
	"errors"
	"net/url"

	"github.com/oops1/gogit/internal/ui/about"
	"github.com/oops1/gogit/internal/version"
)

const (
	projectURL = "https://github.com/oops1/gogit"
	licenseURL = "https://github.com/oops1/gogit/blob/main/LICENSE"
)

var ErrURLNotBrowsable = errors.New("app: only http and https links are opened")

var newAboutView = about.NewView

func (a *App) openAbout() {
	view, err := newAboutView(about.CurrentInfo(version.String()))
	if err != nil {
		a.log.Warn("open about dialog failed", "error", err)
		return
	}
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	view.OnGitHub = func() { a.openBrowser(projectURL) }
	view.OnLicense = func() { a.openBrowser(licenseURL) }
	a.eng.ShowModal(view.Dialog())
	view.Restyle(themeFor(a.EffectiveTheme()))
}

func (a *App) openBrowser(raw string) {
	if err := browse(raw); err != nil {
		a.log.Warn("open link failed", "url", raw, "error", err)
	}
}

func browse(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrURLNotBrowsable
	}
	return openURLCommand(raw).Start()
}
