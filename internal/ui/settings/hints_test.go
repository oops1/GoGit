package settings

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func allHintKeys() []string {
	return []string{
		"Dialog.Settings.Language.Hint",
		"Dialog.Settings.Theme.Hint",
		"Dialog.Settings.ShowToolbar.Hint",
		"Dialog.Settings.ShowStatusBar.Hint",
		"Dialog.Settings.ToolbarCaptions.Hint",
		"Dialog.Settings.JournalFullAuthorName.Hint",
		"Dialog.Settings.LogMaxCount.Hint",
		"Dialog.Settings.AutoFetch.Hint",
		"Dialog.Settings.FetchInterval.Hint",
		"Dialog.Settings.DefaultRemote.Hint",
		"Dialog.Settings.PruneOnFetch.Hint",
		"Dialog.Settings.WorkTreeDepth.Hint",
		"Dialog.Settings.PullStrategy.Hint",
		"Dialog.Settings.ShallowDepth.Hint",
	}
}

func TestHintLabelsAreNonEmptyAndLocalizedInBothLanguages(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "ru"} {
		i18n.Apply(lang)
		for _, key := range allHintKeys() {
			h := newHintLabel(key)
			if h.Text() == "" {
				t.Fatalf("hint %q is empty for language %q", key, lang)
			}
			if h.Text() == key {
				t.Fatalf("hint %q falls back to its own key for language %q (missing translation)", key, lang)
			}
		}
	}
}

func TestHintLabelDistinctTextPerKey(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	seen := map[string]string{}
	for _, key := range allHintKeys() {
		text := newHintLabel(key).Text()
		if prev, ok := seen[text]; ok {
			t.Fatalf("hint text %q is shared by %q and %q", text, prev, key)
		}
		seen[text] = key
	}
}

func TestHintLabelUsesSecondaryTextColor(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	h := newHintLabel("Dialog.Settings.Language.Hint")
	if h.TextColor != widget.CurrentTheme().SecondaryText {
		t.Fatalf("hint color = %+v, want the current theme's SecondaryText", h.TextColor)
	}
}

func TestHintLabelApplyThemeAlwaysUsesSecondaryTextNotLabelOrPlaceholder(t *testing.T) {
	h := newHintLabel("Dialog.Settings.Language.Hint")

	secondary := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	theme := &widget.Theme{
		SecondaryText:    secondary,
		LabelText:        color.RGBA{R: 200, G: 200, B: 200, A: 255},
		InputPlaceholder: color.RGBA{R: 100, G: 100, B: 100, A: 255},
	}

	h.ApplyTheme(theme)

	if h.TextColor != secondary {
		t.Fatalf("TextColor = %+v, want SecondaryText %+v", h.TextColor, secondary)
	}
}

func TestAddHintPositionsLabelInTheGridAndSurvivesRelayout(t *testing.T) {
	grid := widget.NewGrid()
	grid.RowDefs = []widget.GridDefinition{
		{Mode: widget.GridSizePixel, Value: 20},
		{Mode: widget.GridSizePixel, Value: 20},
	}
	grid.ColDefs = []widget.GridDefinition{
		{Mode: widget.GridSizePixel, Value: 100},
		{Mode: widget.GridSizePixel, Value: 100},
	}
	grid.SetBounds(image.Rect(0, 0, 200, 40))

	h := addHint(grid, "Dialog.Settings.Language.Hint", 1, 0, 2)
	grid.SetBounds(grid.Bounds())

	if h.Bounds().Empty() {
		t.Fatal("hint label must receive non-empty bounds once the grid lays out")
	}
	if h.Bounds().Min.Y < grid.Bounds().Min.Y+20 {
		t.Fatalf("hint label bounds = %v, want it placed in row 1", h.Bounds())
	}
}
