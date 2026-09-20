package console

import (
	"slices"
	"strings"
)

const maxHistory = 200

type History struct {
	items []string
	pos   int
}

func (h *History) Add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		h.pos = len(h.items)
		return
	}
	if len(h.items) == 0 || h.items[len(h.items)-1] != line {
		h.items = append(h.items, line)
	}
	if len(h.items) > maxHistory {
		h.items = slices.Clone(h.items[len(h.items)-maxHistory:])
	}
	h.pos = len(h.items)
}

func (h *History) Items() []string { return slices.Clone(h.items) }

func (h *History) Previous() (string, bool) {
	if h.pos == 0 {
		return "", false
	}
	h.pos--
	return h.items[h.pos], true
}

func (h *History) Next() (string, bool) {
	if h.pos >= len(h.items) {
		return "", false
	}
	h.pos++
	if h.pos == len(h.items) {
		return "", true
	}
	return h.items[h.pos], true
}
