package filesgrid

import (
	"slices"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/changes"
)

type ColumnID string

const (
	ColName         ColumnID = "name"
	ColState        ColumnID = "state"
	ColRelDir       ColumnID = "rel_dir"
	ColExtension    ColumnID = "extension"
	ColIndexState   ColumnID = "index_state"
	ColModified     ColumnID = "modified"
	ColRelPath      ColumnID = "rel_path"
	ColRenamedFrom  ColumnID = "renamed_from"
	ColSize         ColumnID = "size"
	ColWorkingState ColumnID = "working_state"
)

type columnDef struct {
	id    ColumnID
	key   string
	path  string
	width datagrid.ColumnWidth
}

var columnDefs = []columnDef{
	{ColName, "Files.Column.Name", "Name", datagrid.StarWidth(2)},
	{ColState, "Files.Column.State", "State", datagrid.PixelWidth(110)},
	{ColRelDir, "Files.Column.RelDir", "RelDir", datagrid.StarWidth(2)},
	{ColExtension, "Files.Column.Extension", "Extension", datagrid.PixelWidth(80)},
	{ColIndexState, "Files.Column.IndexState", "IndexState", datagrid.PixelWidth(110)},
	{ColModified, "Files.Column.Modified", "Modified", datagrid.PixelWidth(150)},
	{ColRelPath, "Files.Column.RelPath", "RelPath", datagrid.StarWidth(3)},
	{ColRenamedFrom, "Files.Column.RenamedFrom", "RenamedFrom", datagrid.StarWidth(2)},
	{ColSize, "Files.Column.Size", "Size", datagrid.PixelWidth(90)},
	{ColWorkingState, "Files.Column.WorkingState", "WorkingState", datagrid.PixelWidth(140)},
}

var defaultVisibleIDs = []ColumnID{ColName, ColState, ColRelDir}

func columnByID(id ColumnID) (columnDef, bool) {
	for _, def := range columnDefs {
		if def.id == id {
			return def, true
		}
	}
	return columnDef{}, false
}

func DefaultOrder() []ColumnID {
	ids := make([]ColumnID, len(columnDefs))
	for i, def := range columnDefs {
		ids[i] = def.id
	}
	return ids
}

func DefaultVisible() []ColumnID {
	return slices.Clone(defaultVisibleIDs)
}

func NormalizeOrder(order []ColumnID) []ColumnID {
	result := make([]ColumnID, 0, len(columnDefs))
	seen := map[ColumnID]bool{}
	for _, id := range order {
		if seen[id] {
			continue
		}
		if _, ok := columnByID(id); !ok {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	for _, def := range columnDefs {
		if !seen[def.id] {
			result = append(result, def.id)
		}
	}
	return result
}

func NormalizeVisible(order []ColumnID, visible []ColumnID) []ColumnID {
	known := map[ColumnID]bool{}
	for _, id := range order {
		known[id] = true
	}
	result := make([]ColumnID, 0, len(visible))
	seen := map[ColumnID]bool{}
	for _, id := range visible {
		if seen[id] || !known[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) == 0 {
		return DefaultVisible()
	}
	return result
}

const stateFontScale = 0.85

func drawStateCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(changes.Row)
	if !ok {
		return
	}
	size := cdc.FontSize * stateFontScale
	textY := cdc.Rect.Min.Y + (cdc.Rect.Dy()-int(size*stateLineRatio))/2
	cdc.DrawCtx.DrawTextSize(row.State, cdc.Rect.Min.X+stateCellPadding, textY, size, cdc.TextColor)
}

const (
	stateCellPadding = 4
	stateLineRatio   = 1.4
)
