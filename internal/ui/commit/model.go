package commit

import "strings"

type Model struct {
	Message     string
	Amend       bool
	Staged      int
	Files       int
	LastMessage string
	Merging     bool
	NoVerify    bool
}

func (m Model) CanConfirm() bool {
	return strings.TrimSpace(m.Message) != ""
}
