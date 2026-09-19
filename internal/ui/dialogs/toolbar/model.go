package toolbar

import "slices"

const (
	SeparatorID = "|"
	StretchID   = "<->"
)

type Entry struct {
	ID    string
	Label string
}

func Repeatable(id string) bool {
	return id == SeparatorID || id == StretchID
}

type Model struct {
	catalog  []Entry
	labels   map[string]string
	defaults []string
	selected []string
	captions bool
}

func NewModel(catalog []Entry, selected, defaults []string, captions bool) *Model {
	m := &Model{
		catalog:  slices.Clone(catalog),
		labels:   make(map[string]string, len(catalog)),
		captions: captions,
	}
	for _, entry := range catalog {
		m.labels[entry.ID] = entry.Label
	}
	m.defaults = m.known(defaults)
	m.selected = m.known(selected)
	return m
}

func (m *Model) known(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := m.labels[id]; !ok {
			continue
		}
		if !Repeatable(id) && slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (m *Model) Captions() bool { return m.captions }

func (m *Model) Items() []string { return slices.Clone(m.selected) }

func (m *Model) Defaults() []string { return slices.Clone(m.defaults) }

func (m *Model) Selected() []Entry { return m.entries(m.selected) }

func (m *Model) Available() []Entry {
	ids := make([]string, 0, len(m.catalog))
	for _, entry := range m.catalog {
		if Repeatable(entry.ID) || !slices.Contains(m.selected, entry.ID) {
			ids = append(ids, entry.ID)
		}
	}
	return m.entries(ids)
}

func (m *Model) entries(ids []string) []Entry {
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		out = append(out, Entry{ID: id, Label: m.labels[id]})
	}
	return out
}

func (m *Model) Add(available, at int) int {
	entries := m.Available()
	if available < 0 || available >= len(entries) {
		return -1
	}
	id := entries[available].ID
	if at < 0 || at > len(m.selected) {
		at = len(m.selected)
	}
	m.selected = slices.Insert(m.selected, at, id)
	return at
}

func (m *Model) Remove(at int) bool {
	if at < 0 || at >= len(m.selected) {
		return false
	}
	m.selected = slices.Delete(m.selected, at, at+1)
	return true
}

func (m *Model) Move(from, to int) bool {
	if from < 0 || from >= len(m.selected) || to < 0 || to >= len(m.selected) || from == to {
		return false
	}
	id := m.selected[from]
	m.selected = slices.Delete(m.selected, from, from+1)
	m.selected = slices.Insert(m.selected, to, id)
	return true
}

func (m *Model) Reset() {
	m.selected = slices.Clone(m.defaults)
}

func (m *Model) IsDefault() bool {
	return slices.Equal(m.selected, m.defaults)
}

func (m *Model) HasCommands() bool {
	for _, id := range m.selected {
		if !Repeatable(id) {
			return true
		}
	}
	return false
}
