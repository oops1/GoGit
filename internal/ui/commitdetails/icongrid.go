package commitdetails

import (
	"image"
	"slices"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	iconSize     = 16
	iconPaddingX = 4
	iconTextGap  = 4
	textPaddingX = 6
	textHeight   = 14
)

type queuedIcon struct {
	rect image.Rectangle
	img  image.Image
}

type iconGrid struct {
	*widget.DataGridWidget
	mu      sync.Mutex
	pending []queuedIcon
}

func newIconGrid() *iconGrid {
	return &iconGrid{DataGridWidget: widget.NewDataGridWidget()}
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
	cdc.DrawCtx.DrawTextSize(row.Path, textX, textY, cdc.FontSize, cdc.TextColor)
}
