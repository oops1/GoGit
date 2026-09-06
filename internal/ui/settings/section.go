package settings

import (
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
	v.nav = widget.NewNavPanel()
	v.nav.ExpandedWidth = navWidth
	v.nav.ItemHeight = navItemHeight
	v.nav.IconSize = navIconSize
	v.navItems = newNavItems()
	v.nav.SetItems(v.navItems)
	v.nav.OnSelect = func(index int) {
		if index >= 0 && index < len(sectionOrder) {
			v.SetSection(sectionOrder[index].id)
		}
	}
	v.dlg.SetNavPanel(v.nav)
	v.dlg.SetNavButton(true)
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
	v.syncScroll()
}

func (v *View) syncNavSelection() {
	v.selectNavSection(v.section)
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
