package journal

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func previewRow(name string, parents ...string) Row {
	row := Row{
		Message:   "commit " + name,
		Author:    "Ann Author",
		Date:      "2026-09-08 12:00",
		ShortHash: previewID(name).String()[:shortHashSize],
		ID:        previewID(name),
	}
	for _, parent := range parents {
		row.Parents = append(row.Parents, previewID(parent))
	}
	return row
}

func previewID(name string) hash.ObjectID {
	digits := ""
	for _, r := range name {
		digits += string("0123456789abcdef"[int(r)%16])
	}
	for len(digits) < 40 {
		digits += "0"
	}
	parsed, err := hash.Parse(digits[:40])
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestPreviewCommitGraph(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	rows := []Row{
		previewRow("m", "b", "c"),
		previewRow("b", "d"),
		previewRow("c", "d"),
		previewRow("d", "e"),
		previewRow("e", "f", "g"),
		previewRow("f", "h"),
		previewRow("g", "i"),
		previewRow("h", "j"),
		previewRow("i", "j"),
		previewRow("j", "k"),
		previewRow("k"),
	}
	for _, theme := range []struct {
		name  string
		theme *widget.Theme
	}{
		{"dark", widget.Win11DarkTheme()},
		{"light", widget.Win11LightTheme()},
	} {
		widget.ClearStrings()
		eng := engine.New(520, 300, 30)
		eng.SetTheme(theme.theme)
		root := widget.NewPanel(theme.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 520, 300))

		grid := widget.NewDataGridWidget()
		grid.Grid.SetColumns(journalColumns())
		grid.SetBounds(image.Rect(10, 10, 510, 290))
		root.AddChild(grid)
		eng.SetRoot(root)

		view := NewView()
		view.Bind(grid)
		view.Append(rows)

		eng.SaveFrames(dir + "/graph-" + theme.name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
