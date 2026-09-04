package refs

import (
	"fmt"
	"slices"
	"strings"
)

type Name string

const (
	HEAD           Name = "HEAD"
	FetchHead      Name = "FETCH_HEAD"
	OrigHead       Name = "ORIG_HEAD"
	MergeHead      Name = "MERGE_HEAD"
	CherryPickHead Name = "CHERRY_PICK_HEAD"
	RebaseHead     Name = "REBASE_HEAD"
	BisectHead     Name = "BISECT_HEAD"
)

const (
	RefsPrefix      = "refs/"
	HeadsPrefix     = "refs/heads/"
	TagsPrefix      = "refs/tags/"
	RemotesPrefix   = "refs/remotes/"
	NotesPrefix     = "refs/notes/"
	BisectPrefix    = "refs/bisect/"
	WorktreePrefix  = "refs/worktree/"
	RewrittenPrefix = "refs/rewritten/"
)

const (
	lockSuffix       = ".lock"
	symbolicPrefix   = "ref:"
	forbiddenRefText = " ~^:?[\\"
)

var pseudoRefs = []Name{HEAD, FetchHead, OrigHead, MergeHead, CherryPickHead, RebaseHead, BisectHead}

var perWorktreePrefixes = []string{BisectPrefix, WorktreePrefix, RewrittenPrefix}

type CheckFlags uint8

const (
	AllowOneLevel CheckFlags = 1 << iota
	RefspecPattern
)

func (n Name) String() string { return string(n) }

func (n Name) IsBranch() bool { return strings.HasPrefix(string(n), HeadsPrefix) }

func (n Name) IsTag() bool { return strings.HasPrefix(string(n), TagsPrefix) }

func (n Name) IsRemote() bool { return strings.HasPrefix(string(n), RemotesPrefix) }

func (n Name) IsPseudo() bool { return slices.Contains(pseudoRefs, n) }

func (n Name) IsPerWorktree() bool {
	if n.IsPseudo() {
		return true
	}
	for _, prefix := range perWorktreePrefixes {
		if strings.HasPrefix(string(n), prefix) {
			return true
		}
	}
	return false
}

func (n Name) Short() string {
	for _, prefix := range []string{HeadsPrefix, TagsPrefix, RemotesPrefix} {
		if short, ok := strings.CutPrefix(string(n), prefix); ok {
			return short
		}
	}
	return string(n)
}

func BranchName(short string) Name { return Name(HeadsPrefix + short) }

func TagName(short string) Name { return Name(TagsPrefix + short) }

func RemoteBranchName(remote, short string) Name {
	return Name(RemotesPrefix + remote + "/" + short)
}

func (n Name) Validate() error {
	var flags CheckFlags
	if n.IsPseudo() {
		flags = AllowOneLevel
	}
	return CheckFormat(string(n), flags)
}

func CheckFormat(name string, flags CheckFlags) error {
	if name == "@" {
		return fmt.Errorf("%w: %q is a single at sign", ErrInvalidName, name)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("%w: %q ends with a dot", ErrInvalidName, name)
	}
	components := 0
	for rest := name; ; {
		component, tail, more := strings.Cut(rest, "/")
		if err := checkComponent(component, name, &flags); err != nil {
			return err
		}
		components++
		if !more {
			break
		}
		rest = tail
	}
	if components < 2 && flags&AllowOneLevel == 0 {
		return fmt.Errorf("%w: %q has a single component", ErrInvalidName, name)
	}
	return nil
}

func checkComponent(component, name string, flags *CheckFlags) error {
	if component == "" {
		return fmt.Errorf("%w: %q has an empty component", ErrInvalidName, name)
	}
	if component[0] == '.' {
		return fmt.Errorf("%w: component %q of %q starts with a dot", ErrInvalidName, component, name)
	}
	if strings.HasSuffix(component, lockSuffix) {
		return fmt.Errorf("%w: component %q of %q ends with %q", ErrInvalidName, component, name, lockSuffix)
	}
	var previous byte
	for index := range len(component) {
		current := component[index]
		switch {
		case current == '.' && previous == '.':
			return fmt.Errorf("%w: %q contains two dots", ErrInvalidName, name)
		case current == '{' && previous == '@':
			return fmt.Errorf("%w: %q contains an at sign followed by a brace", ErrInvalidName, name)
		case current == '*':
			if *flags&RefspecPattern == 0 {
				return fmt.Errorf("%w: %q contains an asterisk", ErrInvalidName, name)
			}
			*flags &^= RefspecPattern
		case isForbiddenByte(current):
			return fmt.Errorf("%w: %q contains the byte %q", ErrInvalidName, name, string(rune(current)))
		}
		previous = current
	}
	return nil
}

func isForbiddenByte(current byte) bool {
	return current < 0x20 || current == 0x7f || strings.IndexByte(forbiddenRefText, current) >= 0
}
