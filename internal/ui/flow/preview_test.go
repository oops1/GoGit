package flow

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
)

type previewView interface {
	Dialog() *widget.Dialog
	Restyle(*widget.Theme)
}

func previewFlowDialogs(t *testing.T) map[string]func() (previewView, error) {
	t.Helper()
	finish := func(kind string, known FinishKnown) func() (previewView, error) {
		return func() (previewView, error) {
			v, err := NewFinishView(kind)
			if err == nil {
				v.SetKnown(known)
			}
			return v, err
		}
	}
	return map[string]func() (previewView, error){
		"start": func() (previewView, error) {
			v, err := NewStartView(ops.FlowKindFeature)
			if err == nil {
				v.SetKnown(StartKnown{Base: "develop", Bases: []string{"develop", "main"}, Prefix: "feature/"})
				v.nameBox.SetText("Feature-1")
				v.refresh()
			}
			return v, err
		},
		"finish-feature": finish(ops.FlowKindFeature, featureKnown),
		"finish-release": finish(ops.FlowKindRelease, releaseKnown),
		"finish-hotfix":  finish(ops.FlowKindHotfix, hotfixKnown),
		"config": func() (previewView, error) {
			v, err := NewConfigView()
			if err == nil {
				v.SetRemotes([]string{"origin"})
				v.SetModel(ConfigModelOf(ops.DefaultFlowConfig()))
			}
			return v, err
		},
		"configured": func() (previewView, error) { return NewConfiguredView() },
		"select-log": func() (previewView, error) {
			v, err := NewLogView()
			if err == nil {
				v.SetCommits(twoCommits())
			}
			return v, err
		},
		"integrate": func() (previewView, error) {
			v, err := NewIntegrateView()
			if err == nil {
				v.SetKnown("feature/Feature-1", "develop")
			}
			return v, err
		},
	}
}

func TestPreviewFlowDialogs(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for name, open := range previewFlowDialogs(t) {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply("ru")
		theme := widget.Win11DarkTheme()
		eng := engine.New(760, 620, 30)
		eng.SetTheme(theme)
		root := widget.NewPanel(theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 760, 620))
		eng.SetRoot(root)

		view, err := open()
		if err != nil {
			t.Fatal(err)
		}
		eng.ShowModal(view.Dialog())
		view.Restyle(theme)

		eng.SaveFrames(dir + "/flow-" + name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
