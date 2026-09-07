package about

import (
	"errors"
	"fmt"
	"image/color"
	"runtime"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs"
	"github.com/oops1/gogit/internal/ui/icons"
)

const dialogName = "about"

const (
	logoSize       = 80
	githubIconName = "github"
	githubIconSize = 20
	contentPadding = 24
	gitEngineName  = "Go"
	guiEngineName  = "headless-gui/v3"
	copyrightYear  = 2026
)

var ErrWidgetMissing = errors.New("about: named widget missing")

var loadDialog = dialogs.Load

type Info struct {
	Version      string
	Architecture string
}

type View struct {
	dlg *widget.Dialog

	logo          *widget.ImageWidget
	githubIcon    *widget.ImageWidget
	tagline       *widget.Label
	version       *widget.Label
	platform      *widget.Label
	architecture  *widget.Label
	gitEngine     *widget.Label
	guiEngine     *widget.Label
	summaryFirst  *widget.Label
	summarySecond *widget.Label
	copyright     *widget.Label
	githubBtn     *widget.Button
	licenseBtn    *widget.Button
	okBtn         *widget.Button

	OnClose   func()
	OnGitHub  func()
	OnLicense func()
}

func NewView(info Info) (*View, error) {
	dlg, named, err := loadDialog(dialogName, i18n.Tf("Dialog.About.Title", i18n.T("App.Title")))
	if err != nil {
		return nil, err
	}
	v := &View{dlg: dlg}
	if err := v.bind(named); err != nil {
		return nil, err
	}
	dlg.SetContentPadding(contentPadding)
	v.apply(info)
	v.wire()
	return v, nil
}

func (v *View) Dialog() *widget.Dialog { return v.dlg }

func (v *View) bind(named map[string]widget.Widget) error {
	var ok bool
	if v.logo, ok = named["logo"].(*widget.ImageWidget); !ok {
		return fmt.Errorf("%w: logo", ErrWidgetMissing)
	}
	if v.githubIcon, ok = named["githubIcon"].(*widget.ImageWidget); !ok {
		return fmt.Errorf("%w: githubIcon", ErrWidgetMissing)
	}
	if v.tagline, ok = named["tagline"].(*widget.Label); !ok {
		return fmt.Errorf("%w: tagline", ErrWidgetMissing)
	}
	if v.summaryFirst, ok = named["summaryFirst"].(*widget.Label); !ok {
		return fmt.Errorf("%w: summaryFirst", ErrWidgetMissing)
	}
	if v.summarySecond, ok = named["summarySecond"].(*widget.Label); !ok {
		return fmt.Errorf("%w: summarySecond", ErrWidgetMissing)
	}
	if v.version, ok = named["version"].(*widget.Label); !ok {
		return fmt.Errorf("%w: version", ErrWidgetMissing)
	}
	if v.platform, ok = named["platform"].(*widget.Label); !ok {
		return fmt.Errorf("%w: platform", ErrWidgetMissing)
	}
	if v.architecture, ok = named["architecture"].(*widget.Label); !ok {
		return fmt.Errorf("%w: architecture", ErrWidgetMissing)
	}
	if v.gitEngine, ok = named["gitEngine"].(*widget.Label); !ok {
		return fmt.Errorf("%w: gitEngine", ErrWidgetMissing)
	}
	if v.guiEngine, ok = named["guiEngine"].(*widget.Label); !ok {
		return fmt.Errorf("%w: guiEngine", ErrWidgetMissing)
	}
	if v.copyright, ok = named["copyright"].(*widget.Label); !ok {
		return fmt.Errorf("%w: copyright", ErrWidgetMissing)
	}
	if v.githubBtn, ok = named["github"].(*widget.Button); !ok {
		return fmt.Errorf("%w: github", ErrWidgetMissing)
	}
	if v.licenseBtn, ok = named["license"].(*widget.Button); !ok {
		return fmt.Errorf("%w: license", ErrWidgetMissing)
	}
	if v.okBtn, ok = named["ok"].(*widget.Button); !ok {
		return fmt.Errorf("%w: ok", ErrWidgetMissing)
	}
	return nil
}

func (v *View) apply(info Info) {
	v.logo.Stretch = widget.ImageStretchUniform
	if img := icons.ToolbarPlain("app", logoSize); img != nil {
		v.logo.SetImage(img)
	}
	v.githubIcon.Stretch = widget.ImageStretchUniform
	if mark := icons.ToolbarPlain(githubIconName, githubIconSize); mark != nil {
		v.githubIcon.SetImage(mark)
	}
	v.version.SetText(i18n.Tf("Dialog.About.Version", displayVersion(info.Version)))
	v.platform.SetText(i18n.T("Platform.supported"))
	v.architecture.SetText(architectureLabel(info.Architecture))
	v.gitEngine.SetText(gitEngineName)
	v.guiEngine.SetText(guiEngineName)
	v.copyright.SetText(i18n.Tf("Dialog.About.Copyright", copyrightYear))
	v.Restyle(widget.CurrentTheme())
}

func displayVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

func (v *View) Restyle(t *widget.Theme) {
	v.tagline.TextColor = t.SecondaryText
	v.copyright.TextColor = t.SecondaryText
	v.summaryFirst.TextColor = t.LabelText
	v.summarySecond.TextColor = t.LabelText
	styleAsLink(v.githubBtn, t.Accent)
	styleAsLink(v.licenseBtn, t.SecondaryText)
	v.okBtn.BorderColor = t.Accent
	v.okBtn.TextColor = t.LabelText
}

func styleAsLink(btn *widget.Button, text color.RGBA) {
	btn.TextColor = text
	btn.Background = color.RGBA{}
	btn.BorderColor = color.RGBA{}
	btn.PressedBG = color.RGBA{}
}

func (v *View) wire() {
	v.okBtn.OnClick = v.close
	v.githubBtn.OnClick = func() { call(v.OnGitHub) }
	v.licenseBtn.OnClick = func() { call(v.OnLicense) }
	v.dlg.DefaultAction = v.close
	v.dlg.CancelAction = v.close
}

func (v *View) close() { call(v.OnClose) }

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

func CurrentInfo(version string) Info {
	return Info{Version: version, Architecture: runtime.GOARCH}
}

func architectureLabel(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "ARM64"
	default:
		return goarch
	}
}
