package console

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

var checkoutOptions = []option{
	valued("branch", "b"),
	plain("force", "f"),
	plain("track", "t"),
}

var switchOptions = []option{
	valued("create", "c"),
	plain("force", "f"),
	plain("track", "t"),
}

func runCheckout(ctx context.Context, env Env, args []string) (string, error) {
	return changeBranch(ctx, env, args, checkoutOptions, "branch")
}

func runSwitch(ctx context.Context, env Env, args []string) (string, error) {
	return changeBranch(ctx, env, args, switchOptions, "create")
}

func changeBranch(ctx context.Context, env Env, args []string, specs []option, createLong string) (string, error) {
	opts, err := parseOptions(args, specs)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	if opts.has(createLong) {
		return startBranch(ctx, env, opts.value(createLong), rest, opts)
	}
	if len(rest) != 1 {
		return "", detail(ErrUsage, "checkout")
	}
	if err := ops.Switch(ctx, env.Repo, rest[0], ops.SwitchOptions{Force: opts.has("force")}); err != nil {
		return "", err
	}
	return switchedLine(rest[0]), nil
}

func startBranch(ctx context.Context, env Env, name string, rest []string, opts options) (string, error) {
	if len(rest) > 1 {
		return "", detail(ErrUsage, rest[1])
	}
	start := "HEAD"
	if len(rest) == 1 {
		start = rest[0]
	}
	if _, err := ops.StartBranch(ctx, env.Repo, name, start, ops.StartBranchOptions{
		Force: opts.has("force"),
		Track: opts.has("track"),
	}); err != nil {
		return "", err
	}
	return "Switched to a new branch '" + name + "'", nil
}

func switchedLine(target string) string {
	return "Switched to '" + target + "'"
}
