package commitdetails

import (
	"strconv"
	"strings"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const shortLength = 7

type Change struct {
	Status  string
	Path    string
	Old     string
	Added   int
	Deleted int
}

type File struct {
	Path string
	Size int
}

type Details struct {
	Commit    hash.ObjectID
	Parents   []hash.ObjectID
	Author    string
	AuthorAt  time.Time
	Committer string
	CommitAt  time.Time
	Message   string
	Branches  []string
	Tags      []string
	Changes   []Change
	Files     []File
	MoreFiles bool
}

type ChangeRow struct {
	Status  string
	Path    string
	Changes string
}

type FileRow struct {
	Path string
	Size string
}

type View struct {
	tabs    *widget.TabControl
	info    *widget.Label
	changes *iconGrid
	files   *widget.DataGridWidget
	details Details
}

func NewView(tabs *widget.TabControl) *View {
	v := &View{
		tabs:    tabs,
		info:    widget.NewLabel("", widget.CurrentTheme().LabelText),
		changes: newIconGrid(),
		files:   widget.NewDataGridWidget(),
	}
	v.info.WrapText = true
	v.buildColumns()
	tabs.ClearTabs()
	tabs.AddTab(i18n.T("Details.Tab.Info"), v.info)
	tabs.AddTab(i18n.T("Details.Tab.Changes"), v.changes)
	tabs.AddTab(i18n.T("Details.Tab.Files"), v.files)
	v.Clear()
	return v
}

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Body(v.info)
}

func (v *View) Retitle() {
	for index, key := range []string{"Details.Tab.Info", "Details.Tab.Changes", "Details.Tab.Files"} {
		v.tabs.SetTabHeader(index, i18n.T(key))
	}
	v.buildColumns()
	v.Show(v.details)
}

func (v *View) buildColumns() {
	path := datagrid.NewTemplateColumn(i18n.T("Details.Column.Path"), v.changes.drawPathCell)
	path.SetWidth(datagrid.StarWidth(1))
	lines := datagrid.NewTextColumn(i18n.T("Details.Column.Lines"), "Changes")
	lines.SetWidth(datagrid.PixelWidth(110))
	v.changes.Grid.SetColumns([]datagrid.Column{path, lines})

	filePath := datagrid.NewTextColumn(i18n.T("Details.Column.Path"), "Path")
	filePath.SetWidth(datagrid.StarWidth(1))
	size := datagrid.NewTextColumn(i18n.T("Details.Column.Size"), "Size")
	size.SetWidth(datagrid.PixelWidth(110))
	v.files.Grid.SetColumns([]datagrid.Column{filePath, size})
}

func (v *View) Clear() {
	v.details = Details{}
	v.info.SetText(i18n.T("Details.Empty"))
	v.changes.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(nil))
	v.files.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(nil))
}

func (v *View) Details() Details { return v.details }

func (v *View) Show(details Details) {
	if details.Commit.IsZero() {
		v.Clear()
		return
	}
	v.details = details
	v.info.SetText(infoText(details))
	v.changes.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(changeRows(details)))
	v.files.Grid.SetItemsSource(datagrid.NewObservableCollectionFrom(fileRows(details)))
}

func infoText(details Details) string {
	lines := []string{
		i18n.Tf("Details.Info.Commit", details.Commit.String()),
		i18n.Tf("Details.Info.Author", details.Author, when(details.AuthorAt)),
		i18n.Tf("Details.Info.Committer", details.Committer, when(details.CommitAt)),
	}
	if parents := shortList(details.Parents); parents != "" {
		lines = append(lines, i18n.Tf("Details.Info.Parents", parents))
	}
	if len(details.Branches) > 0 {
		lines = append(lines, i18n.Tf("Details.Info.Branches", strings.Join(details.Branches, ", ")))
	}
	if len(details.Tags) > 0 {
		lines = append(lines, i18n.Tf("Details.Info.Tags", strings.Join(details.Tags, ", ")))
	}
	return strings.Join(lines, "\n") + "\n\n" + strings.TrimRight(details.Message, "\n")
}

func when(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Local().Format("2006-01-02 15:04")
}

func shortList(ids []hash.ObjectID) string {
	short := make([]string, 0, len(ids))
	for _, id := range ids {
		short = append(short, id.String()[:shortLength])
	}
	return strings.Join(short, ", ")
}

func changeRows(details Details) []any {
	rows := make([]any, 0, len(details.Changes))
	for _, change := range details.Changes {
		rows = append(rows, ChangeRow{
			Status:  change.Status,
			Path:    pathOf(change),
			Changes: i18n.Tf("Details.Lines", change.Added, change.Deleted),
		})
	}
	return rows
}

func pathOf(change Change) string {
	if change.Old != "" && change.Old != change.Path {
		return change.Old + " → " + change.Path
	}
	return change.Path
}

func fileRows(details Details) []any {
	rows := make([]any, 0, len(details.Files)+1)
	for _, file := range details.Files {
		rows = append(rows, FileRow{Path: file.Path, Size: strconv.Itoa(file.Size)})
	}
	if details.MoreFiles {
		rows = append(rows, FileRow{Path: i18n.T("Details.MoreFiles")})
	}
	return rows
}
