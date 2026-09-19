package console

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/ops"
)

var diffOptions = []option{
	plain("stat", ""),
	plain("numstat", ""),
	plain("name-only", ""),
}

func runDiff(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, diffOptions)
	if err != nil {
		return "", err
	}
	left, right, err := diffRange(opts.args())
	if err != nil {
		return "", err
	}
	result, err := ops.Compare(ctx, env.Repo, left, right, ops.CompareOptions{})
	if err != nil {
		return "", err
	}
	return formatDiff(result.Changes, opts), nil
}

func diffRange(specs []string) (left, right string, err error) {
	switch len(specs) {
	case 1:
		return specs[0], "HEAD", nil
	case 2:
		return specs[0], specs[1], nil
	default:
		return "", "", detail(ErrUsage, "diff")
	}
}

func formatDiff(files []diff.File, opts options) string {
	var out strings.Builder
	switch {
	case opts.has("name-only"):
		return strings.Join(changedPaths(files), "\n")
	case opts.has("numstat"):
		_ = diff.NumStat(&out, files)
	case opts.has("stat"):
		_ = diff.Stat(&out, files, diff.Options{})
	default:
		for _, file := range files {
			_ = diff.Unified(&out, file, diff.Options{})
		}
	}
	return strings.TrimSuffix(out.String(), "\n")
}

func changedPaths(files []diff.File) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := file.NewPath
		if path == "" {
			path = file.OldPath
		}
		paths = append(paths, path)
	}
	return paths
}
