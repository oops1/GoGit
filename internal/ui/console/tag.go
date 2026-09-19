package console

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

var tagOptions = []option{
	plain("annotate", "a"),
	plain("delete", "d"),
	plain("force", "f"),
	plain("list", "l"),
	valued("message", "m"),
}

func runTag(ctx context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, tagOptions)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	switch {
	case opts.has("delete"):
		return deleteTags(ctx, env, rest)
	case len(rest) > 0 && !opts.has("list"):
		return createTag(ctx, env, rest, opts)
	}
	return listTags(ctx, env)
}

func deleteTags(ctx context.Context, env Env, names []string) (string, error) {
	if len(names) == 0 {
		return "", detail(ErrUsage, "tag")
	}
	lines := make([]string, 0, len(names))
	for _, name := range names {
		if err := ops.DeleteTag(ctx, env.Repo, name); err != nil {
			return "", err
		}
		lines = append(lines, "Deleted tag "+name)
	}
	return strings.Join(lines, "\n"), nil
}

func createTag(ctx context.Context, env Env, names []string, opts options) (string, error) {
	if len(names) > 2 {
		return "", detail(ErrUsage, names[2])
	}
	target := "HEAD"
	if len(names) == 2 {
		target = names[1]
	}
	message := opts.value("message")
	if opts.has("annotate") && message == "" {
		return "", detail(ErrUsage, "tag")
	}
	result, err := ops.CreateTag(ctx, env.Repo, names[0], target, ops.CreateTagOptions{
		Message: message,
		Force:   opts.has("force"),
		When:    env.when(),
	})
	if err != nil {
		return "", err
	}
	return "Created tag " + result.Name + " at " + result.Target.String()[:shortHashLength], nil
}

func listTags(ctx context.Context, env Env) (string, error) {
	tags, err := ops.Tags(ctx, env.Repo)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(tags))
	for _, tag := range tags {
		lines = append(lines, tag.Name)
	}
	return strings.Join(lines, "\n"), nil
}
