package filesgrid

import (
	"image"
	"slices"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

const (
	DefaultRowHeight    = 20
	DefaultHeaderHeight = 24
	dragThreshold       = 4
)

type Grid struct {
	dg   *widget.DataGridWidget
	menu *widget.PopupMenu

	mu      sync.Mutex
	order   []ColumnID
	visible map[ColumnID]bool
	capture widget.CaptureManager

	press pressState

	pendingIcons []pendingIcon

	OnColumnsChanged func(order []ColumnID, visible []ColumnID)
}

type pressState struct {
	active bool
	moved  bool
	startX int
	colIdx int
}

func New() *Grid {
	dg := widget.NewDataGridWidget()
	dg.Grid.CanUserSortColumns = false
	dg.Grid.CanUserResizeColumns = false
	dg.Grid.IsReadOnly = true
	dg.Grid.RowHeight = DefaultRowHeight
	dg.Grid.HeaderHeight = DefaultHeaderHeight
	g := &Grid{dg: dg, menu: widget.NewPopupMenu(), press: pressState{colIdx: -1}}
	g.SetColumns(DefaultOrder(), DefaultVisible())
	return g
}

func (g *Grid) Data() *widget.DataGridWidget { return g.dg }

func (g *Grid) SetItemsSource(oc *datagrid.ObservableCollection) { g.dg.Grid.SetItemsSource(oc) }

func (g *Grid) SetOnSelectionChanged(fn func(datagrid.SelectionChangedEvent)) {
	g.dg.Grid.OnSelectionChanged = fn
}

func (g *Grid) Columns() (order []ColumnID, visible []ColumnID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	order = slices.Clone(g.order)
	for _, id := range g.order {
		if g.visible[id] {
			visible = append(visible, id)
		}
	}
	return order, visible
}

func (g *Grid) SetColumns(order []ColumnID, visible []ColumnID) {
	norm := NormalizeOrder(order)
	normVisible := NormalizeVisible(norm, visible)
	visibleSet := make(map[ColumnID]bool, len(normVisible))
	for _, id := range normVisible {
		visibleSet[id] = true
	}
	g.mu.Lock()
	g.order = norm
	g.visible = visibleSet
	g.mu.Unlock()
	g.rebuildColumns()
}

func (g *Grid) rebuildColumns() {
	g.mu.Lock()
	order := slices.Clone(g.order)
	visible := g.visible
	g.mu.Unlock()
	cols := make([]datagrid.Column, 0, len(order))
	for _, id := range order {
		if !visible[id] {
			continue
		}
		def, _ := columnByID(id)
		var col datagrid.Column
		switch id {
		case ColName:
			col = datagrid.NewTemplateColumn(i18n.T(def.key), g.drawNameCell)
		case ColState:
			col = datagrid.NewTemplateColumn(i18n.T(def.key), drawStateCell)
		default:
			col = datagrid.NewTextColumn(i18n.T(def.key), def.path)
		}
		col.SetWidth(def.width)
		cols = append(cols, col)
	}
	g.dg.Grid.SetColumns(cols)
}

func (g *Grid) Retranslate() {
	g.rebuildColumns()
}

func (g *Grid) notifyColumnsChanged() {
	if g.OnColumnsChanged == nil {
		return
	}
	order, visible := g.Columns()
	g.OnColumnsChanged(order, visible)
}

func (g *Grid) Draw(ctx widget.DrawContext) {
	g.mu.Lock()
	g.pendingIcons = g.pendingIcons[:0]
	g.mu.Unlock()

	g.dg.Draw(ctx)

	g.mu.Lock()
	pending := slices.Clone(g.pendingIcons)
	g.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	ctx.SetClip(g.dataRect())
	for _, pi := range pending {
		ctx.DrawImageScaled(pi.img, pi.rect.Min.X, pi.rect.Min.Y, pi.rect.Dx(), pi.rect.Dy())
	}
	ctx.ClearClip()
}

func (g *Grid) dataRect() image.Rectangle {
	b := g.dg.Bounds()
	return image.Rect(b.Min.X, b.Min.Y+g.dg.Grid.HeaderHeight, b.Max.X, b.Max.Y)
}

func (g *Grid) Bounds() image.Rectangle {
	b := g.dg.Bounds()
	if g.menu.IsOpen() {
		return b.Union(g.menu.Bounds())
	}
	return b
}

func (g *Grid) BaseBounds() image.Rectangle { return g.dg.Bounds() }

func (g *Grid) SetBounds(r image.Rectangle) { g.dg.SetBounds(r) }

func (g *Grid) Children() []widget.Widget { return g.dg.Children() }

func (g *Grid) AddChild(w widget.Widget) { g.dg.AddChild(w) }

func (g *Grid) IsEnabled() bool { return g.dg.IsEnabled() }

func (g *Grid) SetEnabled(v bool) { g.dg.SetEnabled(v) }

func (g *Grid) SetFocused(v bool) { g.dg.SetFocused(v) }

func (g *Grid) IsFocused() bool { return g.dg.IsFocused() }

func (g *Grid) SetToolTip(s string) { g.dg.SetToolTip(s) }

func (g *Grid) GetToolTip() string { return g.dg.GetToolTip() }

func (g *Grid) NeedsAnimation() bool { return g.dg.NeedsAnimation() }

func (g *Grid) SetCaptureManager(cm widget.CaptureManager) {
	g.mu.Lock()
	g.capture = cm
	g.mu.Unlock()
}

func (g *Grid) ApplyTheme(t *widget.Theme) {
	g.dg.ApplyTheme(t)
	g.dg.Grid.AlternateBG = g.dg.Grid.Background
	g.menu.ApplyTheme(t)
}

func (g *Grid) HasOverlay() bool { return g.menu.IsOpen() }

func (g *Grid) DrawOverlay(ctx widget.DrawContext) {
	if g.menu.IsOpen() {
		g.menu.DrawOverlay(ctx)
	}
}

func (g *Grid) OverlayBounds() image.Rectangle {
	if g.menu.IsOpen() {
		return g.menu.OverlayBounds()
	}
	return image.Rectangle{}
}

func (g *Grid) Dismiss() {
	if g.menu.IsOpen() {
		g.menu.Close()
	}
}
