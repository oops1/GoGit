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
	noFastForward = "no-ff"
	stateFileMode = 0o666
)

var mergeStateFiles = []string{mergeHeadFile, mergeMsgFile, mergeModeFile, autoMergeFile, squashMsgFile}

type MergeState struct {
	Heads         []hash.ObjectID
	Message       string
	NoFastForward bool
}

func (s MergeState) InProgress() bool { return len(s.Heads) > 0 }

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
	var errs []error
	for _, name := range mergeStateFiles {
		if err := fsRootRemove(r.Root(), name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("ops: remove %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
