package console

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/ui/branches"
)

const remoteBranchPrefix = "remotes/"

var branchOptions = []option{
	plain("all", "a"),
	plain("remotes", "r"),
	plain("verbose", "v"),
	plain("delete", "d"),
	plain("force-delete", "D"),
	plain("move", "m"),
	plain("force-move", "M"),
	plain("force", "f"),
}

func runBranch(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, branchOptions)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	switch {
	case opts.anyOf("delete", "force-delete"):
		return deleteBranches(ctx, env, rest, opts.has("force-delete"))
	case opts.anyOf("move", "force-move"):
		return moveBranch(ctx, env, rest, opts.has("force-move"))
	case len(rest) > 0:
		return createBranch(ctx, env, rest, opts.has("force"))
	}
	return listBranches(env, opts)
}

func deleteBranches(ctx context.Context, env Env, names []string, force bool) (string, error) {
	if len(names) == 0 {
		return "", detail(ErrUsage, "branch")
	}
	var done []string
	for _, name := range names {
		if err := ops.DeleteBranch(ctx, env.Repo, name, force); err != nil {
			return "", err
		}
		done = append(done, "Deleted branch "+name)
	}
	return strings.Join(done, "\n"), nil
}

func moveBranch(ctx context.Context, env Env, names []string, force bool) (string, error) {
	var from, to string
	switch len(names) {
	case 1:
		snap, err := env.snapshot()
		if err != nil {
			return "", err
		}
		if snap.Detached {
			return "", detail(ErrUsage, names[0])
		}
		from, to = snap.Current, names[0]
	case 2:
		from, to = names[0], names[1]
	default:
		return "", detail(ErrUsage, "branch")
	}
	if err := ops.RenameBranch(ctx, env.Repo, from, to, force); err != nil {
		return "", err
	}
	return "Renamed branch " + from + " to " + to, nil
}

func createBranch(ctx context.Context, env Env, names []string, force bool) (string, error) {
	if len(names) > 2 {
		return "", detail(ErrUsage, names[2])
	}
	start := "HEAD"
	if len(names) == 2 {
		start = names[1]
	}
	id, err := env.resolve(start)
	if err != nil {
		return "", err
	}
	if err := ops.CreateBranch(ctx, env.Repo, names[0], id, ops.CreateBranchOptions{Force: force}); err != nil {
		return "", err
	}
	return "Created branch " + names[0] + " at " + id.String()[:shortHashLength], nil
}

func listBranches(env Env, opts options) (string, error) {
	snap, err := env.snapshot()
	if err != nil {
		return "", err
	}
	var db *odb.DB
	if opts.has("verbose") {
		if db, err = env.openObjects(); err != nil {
			return "", err
		}
		defer func() { _ = db.Close() }()
	}
	var lines []string
	if !opts.has("remotes") {
		lines = append(lines, localBranchLines(snap, db)...)
	}
	if opts.anyOf("all", "remotes") {
		lines = append(lines, remoteBranchLines(snap, db)...)
	}
	return strings.Join(lines, "\n"), nil
}

func localBranchLines(snap branches.Snapshot, db *odb.DB) []string {
	lines := make([]string, 0, len(snap.Local))
	for _, branch := range snap.Local {
		short := branch.Name.Short()
		marker := "  "
		if !snap.Detached && short == snap.Current {
			marker = "* "
		}
		lines = append(lines, marker+branchLabel(short, branch, db))
	}
	return lines
}

func remoteBranchLines(snap branches.Snapshot, db *odb.DB) []string {
	var lines []string
	for _, remote := range snap.Remotes {
		for _, branch := range remote.Branches {
			short := remoteBranchPrefix + strings.TrimPrefix(branch.Name.String(), "refs/remotes/")
			lines = append(lines, "  "+branchLabel(short, branch, db))
		}
	}
	return lines
}

func branchLabel(short string, branch branches.Branch, db *odb.DB) string {
	if db == nil {
		return short
	}
	label := short + " " + branch.Target.String()[:shortHashLength]
	if subject := commitSubject(db, branch.Target); subject != "" {
		label += " " + subject
	}
	return label
}
