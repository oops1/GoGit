package console

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

var submoduleOptions = []option{
	plain("init", ""),
	plain("recursive", ""),
	plain("remote", ""),
	plain("force", ""),
}

func runSubmodule(ctx context.Context, env Env, args []string) (string, error) {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	opts, err := parseOptions(args, submoduleOptions)
	if err != nil {
		return "", err
	}
	paths := opts.args()
	switch sub {
	case "status":
		return submoduleStatus(ctx, env)
	case "init":
		return submoduleInit(ctx, env, paths)
	case "sync":
		return submoduleSync(ctx, env, paths, opts)
	case "update":
		return submoduleUpdate(ctx, env, paths, opts)
	}
	return "", detail(ErrUsage, sub)
}

func submoduleStatus(ctx context.Context, env Env) (string, error) {
	items, err := ops.ListSubmodules(ctx, env.Repo)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, submoduleMarker(item)+item.Recorded.String()+" "+item.Path)
	}
	return strings.Join(lines, "\n"), nil
}

func submoduleMarker(item ops.Submodule) string {
	switch item.State {
	case ops.SubmoduleStateNotInitialized:
		return "-"
	case ops.SubmoduleStateNewCommits:
		return "+"
	case ops.SubmoduleStateConflict:
		return "U"
	default:
		return " "
	}
}

func submoduleInit(ctx context.Context, env Env, paths []string) (string, error) {
	if err := ops.SubmoduleInit(ctx, env.Repo, paths, ops.SubmoduleInitOptions{}); err != nil {
		return "", err
	}
	return "Initialised submodules", nil
}

func submoduleSync(ctx context.Context, env Env, paths []string, opts options) (string, error) {
	if err := ops.SubmoduleSync(ctx, env.Repo, paths, ops.SubmoduleSyncOptions{Recursive: opts.has("recursive")}); err != nil {
		return "", err
	}
	return "Synchronised submodule URLs", nil
}

func submoduleUpdate(ctx context.Context, env Env, paths []string, opts options) (string, error) {
	err := ops.SubmoduleUpdate(ctx, env.Repo, paths, ops.SubmoduleUpdateOptions{
		Init:      opts.has("init"),
		Recursive: opts.has("recursive"),
		Remote:    opts.has("remote"),
		Force:     opts.has("force"),
		Transport: env.network(),
	})
	if err != nil {
		return "", err
	}
	return "Updated submodules", nil
}
