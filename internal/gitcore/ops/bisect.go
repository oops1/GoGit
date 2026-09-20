package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	bisectStartFile = "BISECT_START"
	bisectLogFile   = "BISECT_LOG"
	bisectHeadFile  = "BISECT_HEAD"
	branchRefPrefix = "refs/heads/"
	symbolicPrefix  = "ref:"
)

var bisectStateFiles = []string{bisectHeadFile, "BISECT_EXPECTED_REV", "BISECT_ANCESTORS_OK", bisectLogFile, "BISECT_NAMES", "BISECT_RUN", "BISECT_TERMS", "BISECT_FIRST_PARENT"}

func bisectOrigin(start string) string {
	return strings.TrimPrefix(strings.TrimSpace(start), branchRefPrefix)
}

func stateFileExists(r *repo.Repository, name string) (bool, error) {
	_, err := fsRootLstat(r.Root(), name)
	if err == nil || missingPath(err) {
		return err == nil, nil
	}
	return false, fmt.Errorf("ops: stat %s: %w", name, err)
}

func readBisectState(r *repo.Repository, state *MergeState) error {
	bisecting, err := stateFileExists(r, bisectLogFile)
	if err != nil || !bisecting {
		return err
	}
	start, err := readStateFile(r, bisectStartFile)
	state.Bisecting, state.BisectStart = true, bisectOrigin(start)
	return err
}

func ResetBisect(ctx context.Context, r *repo.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start, err := readStateFile(r, bisectStartFile)
	if err != nil {
		return err
	}
	origin := bisectOrigin(start)
	if origin == "" {
		return ErrNotBisecting
	}
	noCheckout, err := stateFileExists(r, bisectHeadFile)
	if err != nil {
		return err
	}
	if !noCheckout {
		if err := Switch(ctx, r, origin, SwitchOptions{}); err != nil {
			return err
		}
	}
	return clearBisectState(r)
}

func clearBisectState(r *repo.Repository) error {
	rc, err := openRepoContext(r)
	if err != nil {
		return err
	}
	defer func() { _ = rc.close() }()
	return clearBisectStateWith(rc)
}

func bisectOf(gitDir string) (origin string, detached, bisecting bool) {
	if _, err := os.Stat(filepath.Join(gitDir, bisectLogFile)); err != nil {
		return "", false, false
	}
	start, _ := os.ReadFile(filepath.Join(gitDir, bisectStartFile))
	head, _ := os.ReadFile(filepath.Join(gitDir, string(refs.HEAD)))
	return bisectOrigin(string(start)), !strings.HasPrefix(string(head), symbolicPrefix), true
}

func refuseBisectedBranch(r *repo.Repository, branch refs.Name, onlyDetached bool, refusal error) error {
	dirs, err := gitDirsOf(r)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		origin, detached, bisecting := bisectOf(dir)
		if bisecting && refs.BranchName(origin) == branch && (detached || !onlyDetached) {
			return fmt.Errorf("%w: %s is being bisected in %s", refusal, branch.Short(), dir)
		}
	}
	return nil
}
