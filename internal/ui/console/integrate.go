package console

import (
	"context"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

var mergeOptions = []option{
	plain("no-ff", ""),
	plain("ff-only", ""),
	plain("squash", ""),
	plain("no-commit", ""),
	valued("message", "m"),
}

var rebaseOptions = []option{
	plain("continue", ""),
	plain("skip", ""),
	plain("abort", ""),
	valued("onto", ""),
}

func runMerge(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, mergeOptions)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	if len(rest) != 1 {
		return "", detail(ErrUsage, "merge")
	}
	mode, err := mergeMode(opts)
	if err != nil {
		return "", err
	}
	result, err := ops.Merge(ctx, env.Repo, rest[0], ops.MergeOptions{
		Mode:     mode,
		NoCommit: opts.has("no-commit"),
		Message:  opts.value("message"),
		When:     env.when(),
	})
	if err != nil {
		return "", err
	}
	return formatMerge(result), nil
}

func mergeMode(opts options) (ops.MergeMode, error) {
	chosen := 0
	mode := ops.MergeFastForward
	if opts.has("no-ff") {
		chosen++
		mode = ops.MergeNoFastForward
	}
	if opts.has("ff-only") {
		chosen++
		mode = ops.MergeFastForwardOnly
	}
	if opts.has("squash") {
		chosen++
		mode = ops.MergeSquash
	}
	if chosen > 1 {
		return mode, detail(ErrUsage, "merge")
	}
	return mode, nil
}

func formatMerge(result ops.MergeResult) string {
	var lines []string
	switch {
	case result.UpToDate:
		lines = append(lines, "Already up to date.")
	case result.FastForward:
		lines = append(lines, "Fast-forward "+result.Old.String()[:shortHashLength]+".."+result.New.String()[:shortHashLength])
	case result.Committed:
		lines = append(lines, "Merge made by the 'ort' strategy: "+result.New.String()[:shortHashLength])
	}
	lines = append(lines, conflictLines(result.Conflicts)...)
	return strings.Join(lines, "\n")
}

func runRebase(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, rebaseOptions)
	if err != nil {
		return "", err
	}
	if opts.anyOf("continue", "skip", "abort") {
		return resumeRebase(ctx, env, opts)
	}
	rest := opts.args()
	if len(rest) != 1 {
		return "", detail(ErrUsage, "rebase")
	}
	result, err := ops.Rebase(ctx, env.Repo, rest[0], ops.RebaseOptions{Onto: opts.value("onto"), When: env.when()})
	if err != nil {
		return "", err
	}
	return formatRebase(result), nil
}

func resumeRebase(ctx context.Context, env Env, opts options) (string, error) {
	if opts.has("abort") {
		if err := ops.AbortOperation(ctx, env.Repo); err != nil {
			return "", err
		}
		return "Rebase aborted", nil
	}
	step := ops.ContinueRebase
	if opts.has("skip") {
		step = ops.SkipRebase
	}
	result, err := step(ctx, env.Repo, ops.RebaseOptions{When: env.when()})
	if err != nil {
		return "", err
	}
	return formatRebase(result), nil
}

func formatRebase(result ops.RebaseResult) string {
	var lines []string
	switch {
	case result.UpToDate:
		lines = append(lines, "Current branch is up to date.")
	case result.Finished():
		lines = append(lines, "Applied "+strconv.Itoa(result.Applied)+" commit(s)")
	default:
		lines = append(lines, "Stopped at "+result.Stopped.String()[:shortHashLength])
	}
	lines = append(lines, conflictLines(result.Conflicts)...)
	return strings.Join(lines, "\n")
}
