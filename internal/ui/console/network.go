package console

import (
	"context"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

var fetchOptions = []option{
	plain("prune", "p"),
	plain("tags", "t"),
}

var pullOptions = []option{
	plain("prune", "p"),
}

var pushOptions = []option{
	plain("force", "f"),
	plain("follow-tags", ""),
	plain("atomic", ""),
}

func remoteName(rest []string) (string, error) {
	switch len(rest) {
	case 0:
		return "", nil
	case 1:
		return rest[0], nil
	default:
		return "", detail(ErrUsage, rest[1])
	}
}

func runFetch(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, fetchOptions)
	if err != nil {
		return "", err
	}
	name, err := remoteName(opts.args())
	if err != nil {
		return "", err
	}
	result, err := ops.Fetch(ctx, env.Repo, name, remote.FetchOptions{
		Prune:     opts.has("prune"),
		Tags:      tagMode(opts.has("tags")),
		Transport: env.network(),
	})
	if err != nil {
		return "", err
	}
	return formatFetch(result), nil
}

func tagMode(all bool) remote.TagMode {
	if all {
		return remote.TagsAll
	}
	return remote.TagsFollow
}

func formatFetch(result remote.FetchResult) string {
	lines := []string{"Objects: " + strconv.Itoa(result.Objects)}
	lines = append(lines, changeLines(result.Changes)...)
	return strings.Join(lines, "\n")
}

func changeLines(changes []remote.Change) []string {
	lines := make([]string, 0, len(changes))
	for _, change := range changes {
		lines = append(lines, changeMarker(change)+" "+change.Name.String())
	}
	return lines
}

func changeMarker(change remote.Change) string {
	switch {
	case change.Deleted:
		return "-"
	case change.Created:
		return "*"
	case change.Forced:
		return "+"
	default:
		return " "
	}
}

func runPull(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, pullOptions)
	if err != nil {
		return "", err
	}
	name, err := remoteName(opts.args())
	if err != nil {
		return "", err
	}
	result, err := ops.Pull(ctx, env.Repo, ops.PullOptions{
		Remote: name,
		Fetch:  remote.FetchOptions{Prune: opts.has("prune"), Transport: env.network()},
		When:   env.when(),
	})
	if err != nil {
		return "", err
	}
	return formatPull(result), nil
}

func formatPull(result ops.PullResult) string {
	lines := []string{"Objects: " + strconv.Itoa(result.Fetch.Objects)}
	switch {
	case result.UpToDate:
		lines = append(lines, "Already up to date.")
	case result.Updated:
		lines = append(lines, result.Old.String()[:shortHashLength]+".."+result.New.String()[:shortHashLength])
	}
	lines = append(lines, conflictLines(result.Merge.Conflicts)...)
	return strings.Join(lines, "\n")
}

func conflictLines(paths []string) []string {
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		lines = append(lines, "CONFLICT: "+path)
	}
	return lines
}

func runPush(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, pushOptions)
	if err != nil {
		return "", err
	}
	name, wanted := splitPushTarget(opts.args())
	specs, err := pushRefspecs(env, wanted)
	if err != nil {
		return "", err
	}
	result, err := ops.Push(ctx, env.Repo, name, remote.PushOptions{
		Refspecs:   specs,
		Force:      opts.has("force"),
		FollowTags: opts.has("follow-tags"),
		Atomic:     opts.has("atomic"),
		Transport:  env.network(),
	})
	if err != nil {
		return "", err
	}
	return formatPush(result), nil
}

func splitPushTarget(rest []string) (string, []string) {
	if len(rest) == 0 {
		return "", nil
	}
	return rest[0], rest[1:]
}

func pushRefspecs(env Env, wanted []string) ([]refspec.RefSpec, error) {
	if len(wanted) == 0 {
		snap, err := env.snapshot()
		if err != nil {
			return nil, err
		}
		if snap.Detached {
			return nil, detail(ErrUsage, "push")
		}
		branch := refs.BranchName(snap.Current).String()
		wanted = []string{branch + ":" + branch}
	}
	return refspec.ParseAll(wanted)
}

func formatPush(result remote.PushResult) string {
	lines := make([]string, 0, len(result.Changes)+len(result.Rejected))
	for _, change := range result.Changes {
		lines = append(lines, changeMarker(change)+" "+change.Source.Short()+" -> "+change.Pushed.Short())
	}
	for _, rejected := range result.Rejected {
		lines = append(lines, "! "+rejected.Name+" "+rejected.Message)
	}
	if len(lines) == 0 {
		lines = append(lines, "Everything up-to-date")
	}
	return strings.Join(lines, "\n")
}
