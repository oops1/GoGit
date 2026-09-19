package console

import (
	"context"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

var stageForCommit = ops.Stage

var addOptions = []option{
	plain("force", "f"),
	plain("all", "A"),
	plain("update", "u"),
}

var commitOptions = []option{
	valued("message", "m"),
	plain("amend", ""),
	plain("allow-empty", ""),
	plain("all", "a"),
	plain("no-verify", ""),
}

var resetOptions = []option{
	plain("soft", ""),
	plain("mixed", ""),
	plain("hard", ""),
}

func runAdd(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, addOptions)
	if err != nil {
		return "", err
	}
	paths := opts.args()
	if opts.anyOf("all", "update") {
		if paths, err = env.changedPaths(ctx, opts.has("all")); err != nil {
			return "", err
		}
	}
	if len(paths) == 0 {
		return "", detail(ErrUsage, "add")
	}
	if err := ops.Stage(ctx, env.Repo, paths, ops.StageOptions{Force: opts.has("force")}); err != nil {
		return "", err
	}
	return "Staged " + strconv.Itoa(len(paths)) + " pathspec(s)", nil
}

func (e Env) changedPaths(ctx context.Context, withUntracked bool) ([]string, error) {
	status, err := e.status(ctx)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range status.Entries {
		if stageable(entry, withUntracked) {
			paths = append(paths, strings.TrimSuffix(entry.Path, "/"))
		}
	}
	return paths, nil
}

func stageable(entry worktree.Entry, withUntracked bool) bool {
	switch {
	case entry.Unstaged == worktree.StatusIgnored:
		return false
	case entry.Unstaged == worktree.StatusUntracked:
		return withUntracked
	case entry.IsDir:
		return false
	}
	return entry.Staged != worktree.StatusUnmodified || entry.Unstaged != worktree.StatusUnmodified
}

func runCommit(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, commitOptions)
	if err != nil {
		return "", err
	}
	if rest := opts.args(); len(rest) > 0 {
		return "", detail(ErrUsage, rest[0])
	}
	if !opts.has("message") && !opts.has("amend") {
		return "", detail(ErrUsage, "commit")
	}
	if opts.has("all") {
		paths, pathErr := env.changedPaths(ctx, false)
		if pathErr != nil {
			return "", pathErr
		}
		if len(paths) > 0 {
			if err := stageForCommit(ctx, env.Repo, paths, ops.StageOptions{}); err != nil {
				return "", err
			}
		}
	}
	id, err := ops.Commit(ctx, env.Repo, ops.CommitOptions{
		Message:    opts.value("message"),
		Amend:      opts.has("amend"),
		AllowEmpty: opts.has("allow-empty"),
		When:       env.when(),
		Hooks:      ops.HookOptions{NoVerify: opts.has("no-verify")},
	})
	if err != nil {
		return "", err
	}
	return "[" + id.String()[:shortHashLength] + "] " + subjectOf(opts.value("message")), nil
}

func runReset(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, resetOptions)
	if err != nil {
		return "", err
	}
	mode, err := resetMode(opts)
	if err != nil {
		return "", err
	}
	target, paths := resetTargetAndPaths(opts)
	result, err := ops.Reset(ctx, env.Repo, target, ops.ResetOptions{Mode: mode, Paths: paths, When: env.when()})
	if err != nil {
		return "", err
	}
	return "HEAD is now at " + result.New.String()[:shortHashLength], nil
}

func resetMode(opts options) (ops.ResetMode, error) {
	chosen := 0
	mode := ops.ResetMixed
	if opts.has("soft") {
		chosen++
		mode = ops.ResetSoft
	}
	if opts.has("hard") {
		chosen++
		mode = ops.ResetHard
	}
	if opts.has("mixed") {
		chosen++
		mode = ops.ResetMixed
	}
	if chosen > 1 {
		return mode, detail(ErrUsage, "reset")
	}
	return mode, nil
}

func resetTargetAndPaths(opts options) (string, []string) {
	before, after := opts.split()
	if len(after) > 0 {
		target := "HEAD"
		if len(before) > 0 {
			target = before[0]
		}
		return target, after
	}
	if len(before) == 0 {
		return "HEAD", nil
	}
	return before[0], before[1:]
}
