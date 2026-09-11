package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	mergeHeadFile = "MERGE_HEAD"
	mergeMsgFile  = "MERGE_MSG"
	mergeModeFile = "MERGE_MODE"
	origHeadFile  = "ORIG_HEAD"
	autoMergeFile = "AUTO_MERGE"
	squashMsgFile = "SQUASH_MSG"
	pickHeadFile  = "CHERRY_PICK_HEAD"
	revertFile    = "REVERT_HEAD"
	noFastForward = "no-ff"
	stateFileMode = 0o666
)

var mergeStateFiles = []string{mergeHeadFile, mergeMsgFile, mergeModeFile, autoMergeFile, squashMsgFile, pickHeadFile, revertFile}

type Operation int

const (
	OperationNone Operation = iota
	OperationMerge
	OperationCherryPick
	OperationRevert
	OperationRebase
)

type MergeState struct {
	Heads         []hash.ObjectID
	Picked        hash.ObjectID
	Reverted      hash.ObjectID
	Message       string
	NoFastForward bool
	Rebasing      bool
}

func (s MergeState) Operation() Operation {
	switch {
	case len(s.Heads) > 0:
		return OperationMerge
	case !s.Picked.IsZero():
		return OperationCherryPick
	case !s.Reverted.IsZero():
		return OperationRevert
	case s.Rebasing:
		return OperationRebase
	}
	return OperationNone
}

func (s MergeState) InProgress() bool { return s.Operation() != OperationNone }

func ReadMergeState(r *repo.Repository) (MergeState, error) {
	var state MergeState
	heads, err := readStateFile(r, mergeHeadFile)
	if err != nil {
		return MergeState{}, err
	}
	for line := range strings.FieldsSeq(heads) {
		id, err := hash.Parse(line)
		if err != nil {
			return MergeState{}, fmt.Errorf("ops: %s: %w", mergeHeadFile, err)
		}
		state.Heads = append(state.Heads, id)
	}
	if state.Picked, err = readHeadFile(r, pickHeadFile); err != nil {
		return MergeState{}, err
	}
	if state.Reverted, err = readHeadFile(r, revertFile); err != nil {
		return MergeState{}, err
	}
	headName, err := readStateFile(r, rebasePath(rebaseHeadName))
	if err != nil {
		return MergeState{}, err
	}
	state.Rebasing = headName != ""
	mode, err := readStateFile(r, mergeModeFile)
	if err != nil {
		return MergeState{}, err
	}
	state.NoFastForward = strings.TrimSpace(mode) == noFastForward
	for _, name := range []string{squashMsgFile, mergeMsgFile} {
		text, err := readStateFile(r, name)
		if err != nil {
			return MergeState{}, err
		}
		state.Message += text
	}
	return state, nil
}

func readHeadFile(r *repo.Repository, name string) (hash.ObjectID, error) {
	text, err := readStateFile(r, name)
	if err != nil || strings.TrimSpace(text) == "" {
		return hash.Zero, err
	}
	id, err := hash.Parse(strings.TrimSpace(text))
	if err != nil {
		return hash.Zero, fmt.Errorf("ops: %s: %w", name, err)
	}
	return id, nil
}

func readStateFile(r *repo.Repository, name string) (string, error) {
	data, err := fsRootReadFile(r.Root(), name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("ops: read %s: %w", name, err)
	}
	return string(data), nil
}

func writeStateFile(r *repo.Repository, name, content string) error {
	if err := fsRootWriteFile(r.Root(), name, []byte(content), stateFileMode); err != nil {
		return fmt.Errorf("ops: write %s: %w", name, err)
	}
	return nil
}

func clearMergeState(r *repo.Repository) error {
	return removeStateFiles(r, mergeStateFiles...)
}
