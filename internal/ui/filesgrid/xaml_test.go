package filesgrid

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func withRegisteredTag(t *testing.T) {
	t.Helper()
	Register()
	t.Cleanup(func() { widget.UnregisterXAMLWidget(XAMLTag) })
}

func TestRegisteredTagBuildsAGrid(t *testing.T) {
	withRegisteredTag(t)
	_, named, err := widget.LoadUIFromXAML([]byte(`<Window><FilesGrid x:Name="files"/></Window>`))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := named["files"].(*Grid)
	if !ok {
		t.Fatalf("widget = %T, want *Grid", named["files"])
	}
	if g.Data().Grid.RowHeight != DefaultRowHeight {
		t.Fatalf("RowHeight = %d, want %d", g.Data().Grid.RowHeight, DefaultRowHeight)
	}
}

func TestBuildFromXAMLIgnoresAttributes(t *testing.T) {
	built, err := buildFromXAML(stubAttrs{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := built.(*Grid); !ok {
		t.Fatalf("built = %T, want *Grid", built)
	}
}

type stubAttrs map[string]string

func (a stubAttrs) Attr(names ...string) string {
	for _, name := range names {
		if value, ok := a[name]; ok {
			return value
		}
	}
	return ""
}

func (a stubAttrs) Tag() string { return XAMLTag }

func (a stubAttrs) Text() string { return "" }

func (a stubAttrs) ChildCount() int { return 0 }

func (a stubAttrs) ChildAttrs(int) widget.XAMLAttrs { return a }
