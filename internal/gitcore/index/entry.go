package index

import (
	"os"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type Stage uint16

const (
	StageMerged Stage = iota
	StageAncestor
	StageOurs
	StageTheirs
)

type Stat struct {
	CTime time.Time
	MTime time.Time
	Dev   uint32
	Ino   uint32
	UID   uint32
	GID   uint32
	Size  uint32
}

type Entry struct {
	Path         string
	Mode         object.Mode
	ID           hash.ObjectID
	Stage        Stage
	Stat         Stat
	AssumeValid  bool
	SkipWorktree bool
	IntentToAdd  bool
}

func (s Stage) Valid() bool {
	return s <= StageTheirs
}

func (e *Entry) Extended() bool {
	return e.SkipWorktree || e.IntentToAdd
}

func (e *Entry) Conflicted() bool {
	return e.Stage != StageMerged
}

func (e *Entry) Matches(fi os.FileInfo, racy bool) bool {
	if fi == nil || e.IntentToAdd {
		return false
	}
	if e.SkipWorktree || e.AssumeValid {
		return true
	}
	if !e.typeMatches(fi) {
		return false
	}
	if e.Mode.IsSubmodule() {
		return true
	}
	if uint32(fi.Size()) != e.Stat.Size || !sameTimestamp(e.Stat.MTime, fi.ModTime()) {
		return false
	}
	return !racy || e.Stat.Size == 0
}

func (e *Entry) typeMatches(fi os.FileInfo) bool {
	switch {
	case e.Mode.IsSubmodule():
		return fi.IsDir()
	case e.Mode.IsSymlink():
		return fi.Mode()&os.ModeSymlink != 0
	default:
		return fi.Mode().IsRegular()
	}
}

func sameTimestamp(stored, actual time.Time) bool {
	return uint32(stored.Unix()) == uint32(actual.Unix()) &&
		uint32(stored.Nanosecond()) == uint32(actual.Nanosecond())
}

func comparePathStage(pathA string, stageA Stage, pathB string, stageB Stage) int {
	if order := strings.Compare(pathA, pathB); order != 0 {
		return order
	}
	switch {
	case stageA < stageB:
		return -1
	case stageA > stageB:
		return 1
	default:
		return 0
	}
}
