package ops

import (
	"context"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const deletedPushSource = "(delete)"

func Push(ctx context.Context, r *repo.Repository, remoteName string, opts remote.PushOptions) (remote.PushResult, error) {
	return PushWithHooks(ctx, r, remoteName, opts, HookOptions{})
}

func PushWithHooks(ctx context.Context, r *repo.Repository, remoteName string, opts remote.PushOptions, hookOpts HookOptions) (remote.PushResult, error) {
	if err := ctx.Err(); err != nil {
		return remote.PushResult{}, err
	}
	cfg := r.Config()
	name, err := resolveRemoteName(r, cfg, remoteName)
	if err != nil {
		return remote.PushResult{}, err
	}
	rem, err := remote.Load(cfg, name)
	if err != nil {
		return remote.PushResult{}, err
	}
	if !hookOpts.NoVerify {
		opts.BeforeSend = prePushCheck(openHooks(r, hookOpts), rem)
	}
	result, err := pushRemote(ctx, r, rem, opts)
	if err != nil {
		return result, err
	}
	if err := setPushedUpstreams(r, cfg, name, result.Changes); err != nil {
		return result, err
	}
	return result, nil
}

func prePushCheck(runner hookRunner, rem remote.Remote) func(context.Context, []remote.PushUpdate) error {
	return func(ctx context.Context, updates []remote.PushUpdate) error {
		return runner.verify(ctx, hooks.Invocation{
			Name:  hookPrePush,
			Args:  []string{rem.Name, rem.PushURL()},
			Stdin: runner.prePushInput(updates),
		})
	}
}

func (h hookRunner) prePushInput(updates []remote.PushUpdate) []byte {
	ordered := slices.Clone(updates)
	slices.SortStableFunc(ordered, func(a, b remote.PushUpdate) int {
		switch {
		case !a.Old.IsZero() && !b.Old.IsZero():
			return strings.Compare(string(a.Target), string(b.Target))
		case !a.Old.IsZero():
			return -1
		case !b.Old.IsZero():
			return 1
		}
		return 0
	})
	input := []byte{}
	for _, u := range ordered {
		source := string(u.Source)
		if u.Deleted {
			source = deletedPushSource
		}
		input = append(input, source+" "+h.hex(u.New)+" "+string(u.Target)+" "+h.hex(u.Old)+"\n"...)
	}
	return input
}
