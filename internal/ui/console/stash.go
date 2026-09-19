package console

import (
	"context"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

const stashSelectorPrefix = "stash@{"

var stashPushOptions = []option{
	valued("message", "m"),
	plain("include-untracked", "u"),
	plain("keep-index", "k"),
	plain("staged", "S"),
}

var stashApplyOptions = []option{
	plain("index", ""),
}

func runStash(ctx context.Context, env Env, args []string) (string, error) {
	sub := "push"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list":
		return stashListing(ctx, env, args)
	case "push", "save":
		return stashSave(ctx, env, args)
	case "apply", "pop":
		return stashRestore(ctx, env, args, sub == "pop")
	case "drop":
		return stashRemove(ctx, env, args)
	}
	return "", detail(ErrUsage, sub)
}

func stashListing(ctx context.Context, env Env, args []string) (string, error) {
	if len(args) > 0 {
		return "", detail(ErrUsage, args[0])
	}
	entries, err := ops.StashList(ctx, env.Repo)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Selector()+": "+entry.Message)
	}
	return strings.Join(lines, "\n"), nil
}

func stashSave(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, stashPushOptions)
	if err != nil {
		return "", err
	}
	id, err := ops.StashPush(ctx, env.Repo, ops.StashOptions{
		Message:          opts.value("message"),
		When:             env.when(),
		IncludeUntracked: opts.has("include-untracked"),
		KeepIndex:        opts.has("keep-index"),
		Staged:           opts.has("staged"),
		Paths:            opts.args(),
	})
	if err != nil {
		return "", err
	}
	return "Saved working directory as " + id.String()[:shortHashLength], nil
}

func stashRestore(ctx context.Context, env Env, args []string, pop bool) (string, error) {
	opts, err := parseOptions(args, stashApplyOptions)
	if err != nil {
		return "", err
	}
	position, err := stashPosition(opts.args())
	if err != nil {
		return "", err
	}
	apply := ops.StashApply
	if pop {
		apply = ops.StashPop
	}
	result, err := apply(ctx, env.Repo, position, ops.StashApplyOptions{Index: opts.has("index")})
	if err != nil {
		return "", err
	}
	lines := []string{"Applied " + stashSelector(position)}
	if result.Dropped {
		lines = append(lines, "Dropped "+stashSelector(position))
	}
	lines = append(lines, conflictLines(result.Conflicts)...)
	return strings.Join(lines, "\n"), nil
}

func stashRemove(ctx context.Context, env Env, args []string) (string, error) {
	position, err := stashPosition(args)
	if err != nil {
		return "", err
	}
	if err := ops.StashDrop(ctx, env.Repo, position); err != nil {
		return "", err
	}
	return "Dropped " + stashSelector(position), nil
}

func stashSelector(position int) string {
	return stashSelectorPrefix + strconv.Itoa(position) + "}"
}

func stashPosition(args []string) (int, error) {
	switch len(args) {
	case 0:
		return 0, nil
	case 1:
		return parseStashSelector(args[0])
	default:
		return 0, detail(ErrUsage, args[1])
	}
}

func parseStashSelector(spec string) (int, error) {
	text := spec
	if inner, ok := strings.CutPrefix(text, stashSelectorPrefix); ok {
		text = strings.TrimSuffix(inner, "}")
	}
	position, err := strconv.Atoi(text)
	if err != nil || position < 0 {
		return 0, detail(ErrUsage, spec)
	}
	return position, nil
}
