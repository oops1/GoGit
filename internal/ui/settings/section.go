package settings

import (
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func (v *View) sectionWidgets() map[string]*widget.Grid {
	return map[string]*widget.Grid{
		"general":     v.sectionGeneral,
		"git":         v.sectionGit,
		"credentials": v.sectionCredentials,
		"ssh":         v.sectionSSH,
	}
}

func (v *View) buildNav() {
	v.nav = newNavTree()
	v.nav.Tree.OnSelect = func(item *treeview.TreeViewItem) {
		if id, ok := item.Tag.(string); ok {
			v.SetSection(id)
		}
	}
	v.navHost.AddChild(v.nav)
	v.navHost.SetBounds(v.navHost.Bounds())
	v.SetSection(defaultSectionID)
}

func (v *View) Section() string { return v.section }

func (v *View) SetSection(id string) {
	if !isKnownSection(id) {
		id = defaultSectionID
	}
	if v.section == id {
		return
	}
	v.section = id
	v.applySection()
}

func (v *View) applySection() {
	for id, w := range v.sectionWidgets() {
		w.SetVisible(id == v.section)
	}
	v.sectionTitle.SetText(i18n.T(sectionTitleKey(v.section)))
	v.syncNavSelection()
}

func (v *View) syncNavSelection() {
	v.nav.SetSelected(v.section)
}

func (v *View) Modified() bool {
	return v.request() != v.initial
}

func (v *View) refreshSaveEnabled() {
	v.okBtn.SetEnabled(v.Modified())
}

func (v *View) wireModifiedTracking() {
	onAny := v.refreshSaveEnabled
	v.language.OnChange = func(int, string) { onAny() }
	v.theme.OnChange = func(int, string) { onAny() }
	v.showToolbar.OnChange = func(bool) { onAny() }
	v.toolbarCaptions.OnChange = func(bool) { onAny() }
	v.showStatusBar.OnChange = func(bool) { onAny() }
	v.journalFullAuthorName.OnChange = func(bool) { onAny() }
	v.logMaxCount.OnChange = func(float64) { onAny() }
	v.autoFetch.OnChange = func(bool) { onAny() }
	v.fetchInterval.OnChange = func(float64) { onAny() }
	v.workTreeDepth.OnChange = func(float64) { onAny() }
	v.pullStrategy.OnChange = func(string) { onAny() }
	v.defaultRemote.OnChange = func(string) { onAny() }
	v.pruneOnFetch.OnChange = func(bool) { onAny() }
	v.shallowDepth.OnChange = func(float64) { onAny() }
	v.credentialSource.OnChange = func(int, string) { onAny() }
}
