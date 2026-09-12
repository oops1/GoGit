package commitdetails

import (
	"image"
	"image/color"
	"path"
	"slices"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	iconSize     = 16
	iconPaddingX = 4
	iconTextGap  = 4
	textPaddingX = 6
	textHeight   = 14
	linesGap     = 8
)

type queuedIcon struct {
	rect image.Rectangle
	img  image.Image
}

type linePart struct {
	text  string
	color color.RGBA
}

type iconGrid struct {
	*widget.DataGridWidget
	mu      sync.Mutex
	pending []queuedIcon
	added   color.RGBA
	deleted color.RGBA
	muted   color.RGBA
}

func newIconGrid() *iconGrid {
	g := &iconGrid{DataGridWidget: widget.NewDataGridWidget()}
	g.Restyle(widget.CurrentTheme())
	return g
}

func (g *iconGrid) Restyle(t *widget.Theme) {
	p := style.Of(t)
	g.mu.Lock()
	g.added, g.deleted, g.muted = p.Added(), p.Deleted(), p.Secondary
	g.mu.Unlock()
}

func (g *iconGrid) Draw(ctx widget.DrawContext) {
	g.mu.Lock()
	g.pending = g.pending[:0]
	g.mu.Unlock()

	g.DataGridWidget.Draw(ctx)

	g.mu.Lock()
	queued := slices.Clone(g.pending)
	g.mu.Unlock()
	if len(queued) == 0 {
		return
	}
	b := g.Bounds()
	ctx.SetClip(image.Rect(b.Min.X, b.Min.Y+g.Grid.HeaderHeight, b.Max.X, b.Max.Y))
	for _, q := range queued {
		ctx.DrawImageScaled(q.img, q.rect.Min.X, q.rect.Min.Y, q.rect.Dx(), q.rect.Dy())
	}
	ctx.ClearClip()
}

func (g *iconGrid) queued() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.pending)
}

func (g *iconGrid) drawPathCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(ChangeRow)
	if !ok {
		return
	}
	textX := cdc.Rect.Min.X + textPaddingX
	if img := icons.Status(row.Status, iconSize); img != nil {
		x := cdc.Rect.Min.X + iconPaddingX
		y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-iconSize)/2
		g.mu.Lock()
		g.pending = append(g.pending, queuedIcon{rect: image.Rect(x, y, x+iconSize, y+iconSize), img: img})
		g.mu.Unlock()
		textX = x + iconSize + iconTextGap
	}
	textY := cdc.Rect.Min.Y + (cdc.Rect.Dy()-textHeight)/2
	drawSplitPath(cdc, row.Path, textX, textY, g.mutedColor())
}

func (g *iconGrid) drawFileCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(FileRow)
	if !ok {
		return
	}
	drawSplitPath(cdc, row.Path, cdc.Rect.Min.X+textPaddingX, cdc.Rect.Min.Y+(cdc.Rect.Dy()-textHeight)/2, g.mutedColor())
}

func (g *iconGrid) mutedColor() color.RGBA {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.muted
}

func drawSplitPath(cdc datagrid.CellDrawContext, text string, x, y int, muted color.RGBA) {
	dir, name := path.Split(text)
	if dir != "" {
		cdc.DrawCtx.DrawTextSize(dir, x, y, cdc.FontSize, muted)
		x += cdc.DrawCtx.MeasureText(dir, cdc.FontSize)
	}
	cdc.DrawCtx.DrawTextSize(name, x, y, cdc.FontSize, cdc.TextColor)
}

func (g *iconGrid) drawLinesCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(ChangeRow)
	if !ok {
		return
	}
	g.mu.Lock()
	added, deleted, zero := g.added, g.deleted, g.muted
	g.mu.Unlock()
	x := cdc.Rect.Min.X + textPaddingX
	y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-textHeight)/2
	for _, part := range lineParts(row.Added, row.Deleted, zero, added, deleted) {
		cdc.DrawCtx.DrawTextSize(part.text, x, y, cdc.FontSize, part.color)
		x += cdc.DrawCtx.MeasureText(part.text, cdc.FontSize) + linesGap
	}
}

func lineParts(added, deleted int, zero, addedColor, deletedColor color.RGBA) []linePart {
	if added == 0 && deleted == 0 {
		return nil
	}
	return []linePart{
		{text: i18n.Tf("Details.Lines.Added", added), color: countColor(added, addedColor, zero)},
		{text: i18n.Tf("Details.Lines.Deleted", deleted), color: countColor(deleted, deletedColor, zero)},
	}
}

func countColor(count int, strong, zero color.RGBA) color.RGBA {
	if count > 0 {
		return strong
	}
	return zero
}
