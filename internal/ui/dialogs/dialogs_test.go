package dialogs

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestLoadReturnsDialogAndNamedWidgets(t *testing.T) {
	dlg, named, err := Load("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}
	if dlg == nil {
		t.Fatal("dialog must not be nil")
	}
	if dlg.Title != "Title" {
		t.Fatalf("title = %q", dlg.Title)
	}
	for _, name := range []string{"path", "browse", "hint", "modeOpen", "modeCreate", "bare", "name", "ok", "cancel"} {
		if _, ok := named[name]; !ok {
			t.Fatalf("widget %q missing from loaded xaml", name)
		}
	}
	content := dlg.ContentBounds()
	if content.Dx() <= 0 || content.Dy() <= 0 {
		t.Fatalf("content bounds = %v", content)
	}
}

func installLanguages(t *testing.T) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	t.Cleanup(func() { i18n.Apply("en") })
}

func TestAReleasedDialogNoLongerFollowsTheLanguage(t *testing.T) {
	installLanguages(t)
	released, releasedNamed, err := Load("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}
	kept, keptNamed, err := Load("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}
	english := releasedNamed["browse"].(*widget.Button).GetText()

	Release(released)
	i18n.Apply("ru")

	if got := releasedNamed["browse"].(*widget.Button).GetText(); got != english {
		t.Fatalf("released button = %q, want it left at %q", got, english)
	}
	if got := keptNamed["browse"].(*widget.Button).GetText(); got == english {
		t.Fatalf("kept button = %q, want it translated", got)
	}
	Release(kept)
}

func TestAReleasedResizableDialogNoLongerFollowsTheLanguage(t *testing.T) {
	installLanguages(t)
	dlg, named, err := LoadResizable("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}
	english := named["browse"].(*widget.Button).GetText()

	Release(dlg)
	i18n.Apply("ru")

	if got := named["browse"].(*widget.Button).GetText(); got != english {
		t.Fatalf("released button = %q, want it left at %q", got, english)
	}
}

func TestLoadReturnsErrorWhenDialogNameIsUnknown(t *testing.T) {
	if _, _, err := Load("does-not-exist", "Title"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadReturnsErrorWhenXAMLIsBroken(t *testing.T) {
	prev := source
	source = func(name string) ([]byte, error) { return []byte("<Grid"), nil }
	t.Cleanup(func() { source = prev })

	if _, _, err := Load("broken", "Title"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadPropagatesSourceError(t *testing.T) {
	prev := source
	wantErr := errors.New("boom")
	source = func(name string) ([]byte, error) { return nil, wantErr }
	t.Cleanup(func() { source = prev })

	if _, _, err := Load("whatever", "Title"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAResizableDialogStretchesItsContent(t *testing.T) {
	dlg, _, err := LoadResizable("compare", "Title")
	if err != nil {
		t.Fatal(err)
	}
	dlg.SetResizable(true)
	size := dlg.Bounds()

	dlg.Resize(size.Dx()+200, size.Dy()+120)

	if dlg.Content() == nil || dlg.Content().Bounds() != dlg.ContentBounds() {
		t.Fatalf("content = %v, want it to fill %v", dlg.Content(), dlg.ContentBounds())
	}
}

func TestLoadResizablePropagatesSourceError(t *testing.T) {
	prev := source
	wantErr := errors.New("boom")
	source = func(name string) ([]byte, error) { return nil, wantErr }
	t.Cleanup(func() { source = prev })

	if _, _, err := LoadResizable("whatever", "Title"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAPlainDialogKeepsItsContentWhereItWasPlaced(t *testing.T) {
	dlg, _, err := Load("add_repo", "Title")
	if err != nil {
		t.Fatal(err)
	}

	if dlg.Content() != nil {
		t.Fatal("a plain dialog handed its layout to the stretching content slot")
	}
}
