package dialogs

import (
	"errors"
	"testing"
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
