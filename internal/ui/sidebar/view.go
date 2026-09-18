package sidebar

import (
	"image"
	"image/color"
	"slices"
	"strconv"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/icons"
)

type Section int

const (
	SectionRepositories Section = iota
	SectionWorkingCopy
	SectionIndex
	SectionBranches
	SectionTags
	SectionRemotes
	SectionStash
	SectionSubmodules
)

const (
	NoCount = -1

	navWidth       = 230
	navShare       = 0.2
	navItemHeight  = 30
	navIconSize    = 16
	navFontSize    = 10
	countPadRight  = 12
	countTextShift = 7
	filesShare     = 0.36
	diffShare      = 0.58
	detailsShare   = 0.70
	splitterSize   = 5
	minPane        = 80
)

type sectionInfo struct {
	key  string
	icon func(size int, tint color.RGBA) image.Image
}

var sections = []sectionInfo{
	{key: "Sidebar.Section.Repositories", icon: treeIcon("repository")},
	{key: "Sidebar.Section.WorkingCopy", icon: treeIcon("directory")},
	{key: "Sidebar.Section.Index", icon: menuIcon("stage")},
	{key: "Sidebar.Section.Branches", icon: treeIcon("branch")},
	{key: "Sidebar.Section.Tags", icon: treeIcon("tag")},
	{key: "Sidebar.Section.Remotes", icon: menuIcon("remotes")},
	{key: "Sidebar.Section.Stash", icon: treeIcon("stash")},
	{key: "Sidebar.Section.Submodules", icon: treeIcon("folder")},
}

func treeIcon(name string) func(int, color.RGBA) image.Image {
	return func(size int, tint color.RGBA) image.Image { return icons.TreeTinted(name, size, tint) }
}

func menuIcon(name string) func(int, color.RGBA) image.Image {
	return func(size int, tint color.RGBA) image.Image { return icons.Menu(name, size, tint) }
}

type Parts struct {
	Repositories widget.Widget
	Branches     widget.Widget
	Files        widget.Widget
	Diff         widget.Widget
	Journal      widget.Widget
	Details      widget.Widget
}

type View struct {
	root    *widget.SplitPanel
	nav     *countedNav
	host    *widget.DockPanel
	notYet  *widget.Label
	work    widget.Widget
	splits  []*widget.SplitPanel
	parts   Parts
	section Section
	shown   widget.Widget

	OnSelect func(Section)
}

func NewView() *View {
	v := &View{
		nav:    newCountedNav(),
		host:   widget.NewDockPanel(),
		notYet: widget.NewLabel("", widget.CurrentTheme().SecondaryText),
	}
	v.notYet.TextAlign = widget.TextAlignCenter
	v.root = widget.NewSplitPanel(widget.OrientationHorizontal)
	v.root.Position = navShare
	v.root.SplitterSize = splitterSize
	v.root.MinFirst = navWidth / 2
	v.root.AddChild(v.nav)
	v.root.AddChild(v.host)
	v.nav.OnSelect = func(index int) { v.Select(Section(index)) }
	v.Retitle()
	v.section = SectionWorkingCopy
	v.nav.SetSelected(int(v.section))
	return v
}

func (v *View) Root() *widget.SplitPanel { return v.root }

func (v *View) Section() Section { return v.section }

func (v *View) Retitle() {
	tint := v.nav.TextColor
	items := make([]widget.NavPanelItem, 0, len(sections))
	for _, s := range sections {
		items = append(items, widget.NavPanelItem{Icon: s.icon(navIconSize, tint), Text: i18n.T(s.key)})
	}
	selected := v.nav.Selected()
	v.nav.SetItems(items)
	v.nav.SetSelected(selected)
	v.notYet.SetText(i18n.T("Sidebar.NotYet"))
}

func (v *View) Restyle(t *widget.Theme) {
	v.nav.ApplyTheme(t)
	v.nav.setCountColor(t.SecondaryText)
	for _, part := range v.hiddenParts() {
		widget.ApplyThemeTree(part, t)
	}
	v.notYet.TextColor = t.SecondaryText
	v.Retitle()
}

func (v *View) hiddenParts() []widget.Widget {
	var out []widget.Widget
	for _, part := range []widget.Widget{v.parts.Repositories, v.parts.Branches, v.work, v.notYet} {
		if part != nil && part != v.shown && !slices.Contains(out, part) {
			out = append(out, part)
		}
	}
	return out
}

func (v *View) SetCount(section Section, count int) {
	v.nav.setCount(int(section), count)
}

func (v *View) CountText(section Section) (string, bool) {
	return v.nav.countText(int(section))
}

func (v *View) Mount(parts Parts) {
	v.parts = parts
	lower := split(widget.OrientationVertical, diffShare, parts.Diff, parts.Journal)
	center := split(widget.OrientationVertical, filesShare, parts.Files, lower)
	work := split(widget.OrientationHorizontal, detailsShare, center, parts.Details)
	v.splits = []*widget.SplitPanel{lower, center, work}
	v.work = work
	v.showSection()
}

func (v *View) Unmount() Parts {
	parts := v.parts
	v.clearHost()
	for _, panel := range v.splits {
		panel.ClearChildren()
	}
	v.parts, v.work, v.splits = Parts{}, nil, nil
	return parts
}

func (v *View) Select(section Section) {
	if section < 0 || int(section) >= len(sections) {
		return
	}
	v.section = section
	v.nav.SetSelected(int(section))
	v.showSection()
	if v.OnSelect != nil {
		v.OnSelect(section)
	}
}

func (v *View) showSection() {
	var content widget.Widget
	switch v.section {
	case SectionRepositories:
		content = v.parts.Repositories
	case SectionWorkingCopy, SectionIndex:
		content = v.work
	case SectionBranches, SectionTags, SectionRemotes, SectionSubmodules:
		content = v.parts.Branches
	default:
		content = v.notYet
	}
	if content == nil {
		content = v.notYet
	}
	v.clearHost()
	v.host.AddChild(content)
	v.shown = content
	v.host.SetBounds(v.host.Bounds())
	v.host.Invalidate()
}

func (v *View) clearHost() {
	if v.shown != nil {
		v.host.RemoveChild(v.shown)
		v.shown = nil
	}
}

func split(orientation widget.Orientation, share float64, first, second widget.Widget) *widget.SplitPanel {
	panel := widget.NewSplitPanel(orientation)
	panel.Position = share
	panel.SplitterSize = splitterSize
	panel.MinFirst, panel.MinSecond = minPane, minPane
	panel.AddChild(first)
	panel.AddChild(second)
	return panel
}

type countedNav struct {
	*widget.NavPanel

	mu         sync.Mutex
	counts     map[int]int
	countColor color.RGBA
}

func newCountedNav() *countedNav {
	n := &countedNav{NavPanel: widget.NewNavPanel(), counts: map[int]int{}}
	n.ExpandedWidth = navWidth
	n.ItemHeight = navItemHeight
	n.IconSize = navIconSize
	n.FontSize = navFontSize
	n.countColor = widget.CurrentTheme().SecondaryText
	return n
}

func (n *countedNav) setCount(index, count int) {
	n.mu.Lock()
	n.counts[index] = count
	n.mu.Unlock()
	n.Invalidate()
}

func (n *countedNav) setCountColor(c color.RGBA) {
	n.mu.Lock()
	n.countColor = c
	n.mu.Unlock()
}

func (n *countedNav) countText(index int) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	count, ok := n.counts[index]
	switch {
	case !ok:
		return "", false
	case count == NoCount:
		return "—", true
	default:
		return strconv.Itoa(count), true
	}
}

func (n *countedNav) Draw(ctx widget.DrawContext) {
	n.NavPanel.Draw(ctx)
	if n.IsCollapsed() {
		return
	}
	n.mu.Lock()
	col := n.countColor
	n.mu.Unlock()
	for index := range n.ItemCount() {
		text, ok := n.countText(index)
		if !ok {
			continue
		}
		r := n.ItemRect(index)
		width := ctx.MeasureText(text, navFontSize)
		ctx.DrawTextSize(text, r.Max.X-countPadRight-width, r.Min.Y+r.Dy()/2-countTextShift, navFontSize, col)
	}
}
