package ops

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	hookPreCommit        = "pre-commit"
	hookPrepareCommitMsg = "prepare-commit-msg"
	hookCommitMsg        = "commit-msg"
	hookPostCommit       = "post-commit"
	hookPreMergeCommit   = "pre-merge-commit"
	hookPostMerge        = "post-merge"
	hookPostCheckout     = "post-checkout"
	hookPreRebase        = "pre-rebase"
	hookPostRewrite      = "post-rewrite"
	hookPrePush          = "pre-push"

	commitEditMsgFile = "COMMIT_EDITMSG"

	messageSourceMessage = "message"
	messageSourceMerge   = "merge"
	rewriteAmend         = "amend"
	rewriteRebase        = "rebase"
	branchCheckoutFlag   = "1"
	plainMergeFlag       = "0"
	squashMergeFlag      = "1"
)

type HookOptions struct {
	NoVerify bool
	Events   hooks.Sink
}

type hookRunner struct {
	*hooks.Runner
	r    *repo.Repository
	opts HookOptions
}

func openHooks(r *repo.Repository, opts HookOptions) hookRunner {
	return hookRunner{Runner: hooks.New(r, opts.Events), r: r, opts: opts}
}

func (h hookRunner) commitEnv() []string {
	return []string{"GIT_INDEX_FILE=" + h.r.IndexFile(), "GIT_EDITOR=:"}
}

func (h hookRunner) verify(ctx context.Context, inv hooks.Invocation) error {
	if !h.Present(inv.Name) {
		return nil
	}
	return h.Verify(ctx, inv)
}

func (h hookRunner) verifyCommit(ctx context.Context, name string, args ...string) error {
	return h.verify(ctx, hooks.Invocation{Name: name, Args: args, Env: h.commitEnv()})
}

func (h hookRunner) notify(ctx context.Context, inv hooks.Invocation) {
	if h.Present(inv.Name) {
		_, _ = h.Run(ctx, inv)
	}
}

func (h hookRunner) zero() string {
	return strings.Repeat("0", h.r.ObjectFormat.HexSize())
}

func (h hookRunner) hex(id hash.ObjectID) string {
	if id.IsZero() {
		return h.zero()
	}
	return id.String()
}

func (h hookRunner) editsMessage() bool {
	return h.Present(hookPrepareCommitMsg) || !h.opts.NoVerify && h.Present(hookCommitMsg)
}

func (h hookRunner) editMessage(ctx context.Context, file, message, source string) (string, error) {
	if err := writeStateFile(h.r, file, message); err != nil {
		return "", err
	}
	arg := h.PathArg(h.r.GitPath(file))
	if err := h.verifyCommit(ctx, hookPrepareCommitMsg, arg, source); err != nil {
		return "", err
	}
	if !h.opts.NoVerify {
		if err := h.verifyCommit(ctx, hookCommitMsg, arg); err != nil {
			return "", err
		}
	}
	edited, err := readStateFile(h.r, file)
	if err != nil {
		return "", err
	}
	return normalizeMessage(edited), nil
}
