package ops

import (
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

type StageOptions struct {
	Force bool
}

type DiscardOptions struct {
	RemoveUntracked bool
}

type CommitOptions struct {
	Message    string
	Amend      bool
	AllowEmpty bool
	When       time.Time
	Author     *object.Signature
}

type CreateBranchOptions struct {
	Force bool
}

type SwitchOptions struct {
	Force bool
}
