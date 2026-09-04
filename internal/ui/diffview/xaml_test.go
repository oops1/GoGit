package diffview

import (
	"errors"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func withRegisteredTag(t *testing.T) {
	t.Helper()
	Register()
	t.Cleanup(func() { widget.UnregisterXAMLWidget(XAMLTag) })
}

func TestRegisteredTagBuildsConfiguredDiffView(t *testing.T) {
	withRegisteredTag(t)
	xaml := `<Window><DiffView x:Name="diff" Mode="Unified" FontFamily="Courier New" FontSize="13" RowHeight="22"/></Window>`
	_, named, err := widget.LoadUIFromXAML([]byte(xaml))
	if err != nil {
		t.Fatal(err)
	}
	dv, ok := named["diff"].(*DiffView)
	if !ok {
		t.Fatalf("widget = %T, want *DiffView", named["diff"])
	}
	if dv.Mode() != Unified {
		t.Fatalf("mode = %v", dv.Mode())
	}
	if dv.FontFamily() != "Courier New" || dv.FontSize() != 13 || dv.RowHeight() != 22 {
		t.Fatalf("attributes = %q %v %d", dv.FontFamily(), dv.FontSize(), dv.RowHeight())
	}
}

func TestRegisteredTagUsesDefaultsWithoutAttributes(t *testing.T) {
	withRegisteredTag(t)
	_, named, err := widget.LoadUIFromXAML([]byte(`<Window><DiffView x:Name="diff"/></Window>`))
	if err != nil {
		t.Fatal(err)
	}
	dv := named["diff"].(*DiffView)
	if dv.Mode() != SideBySide || dv.FontFamily() != defaultFontFamily {
		t.Fatalf("defaults = %v %q", dv.Mode(), dv.FontFamily())
	}
}

func TestRegisteredTagRejectsInvalidAttributes(t *testing.T) {
	withRegisteredTag(t)
	cases := map[string]string{
		"mode":            `<Window><DiffView Mode="diagonal"/></Window>`,
		"font size text":  `<Window><DiffView FontSize="big"/></Window>`,
		"font size zero":  `<Window><DiffView FontSize="0"/></Window>`,
		"row height text": `<Window><DiffView RowHeight="tall"/></Window>`,
		"row height zero": `<Window><DiffView RowHeight="0"/></Window>`,
	}
	for name, xaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := widget.LoadUIFromXAML([]byte(xaml)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"SideBySide":   SideBySide,
		"side-by-side": SideBySide,
		"split":        SideBySide,
		"Unified":      Unified,
		"inline":       Unified,
	}
	for text, want := range cases {
		got, err := ParseMode(text)
		if err != nil || got != want {
			t.Fatalf("ParseMode(%q) = %v, %v", text, got, err)
		}
	}
	if _, err := ParseMode("zigzag"); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildFromXAMLReportsAttributeErrors(t *testing.T) {
	if _, err := buildFromXAML(stubAttrs{"FontSize": "-1"}); !errors.Is(err, ErrBadAttr) {
		t.Fatalf("err = %v", err)
	}
	if _, err := buildFromXAML(stubAttrs{"RowHeight": "-1"}); !errors.Is(err, ErrBadAttr) {
		t.Fatalf("err = %v", err)
	}
	if _, err := buildFromXAML(stubAttrs{"Mode": "zigzag"}); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("err = %v", err)
	}
	built, err := buildFromXAML(stubAttrs{"FontName": "Iosevka"})
	if err != nil {
		t.Fatal(err)
	}
	if built.(*DiffView).FontFamily() != "Iosevka" {
		t.Fatalf("font = %q", built.(*DiffView).FontFamily())
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
