package console

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const (
	shortHashLength = 7
	gitDateLayout   = "Mon Jan 2 15:04:05 2006 -0700"
	messageIndent   = "    "
)

var logOptions = []option{
	plain("oneline", ""),
	plain("reverse", ""),
	plain("first-parent", ""),
	valued("max-count", "n"),
	valued("skip", ""),
}

var shortCountPattern = regexp.MustCompile(`^-[0-9]+$`)

func splitShortCount(args []string) ([]string, string) {
	kept := make([]string, 0, len(args))
	count := ""
	for _, arg := range args {
		if shortCountPattern.MatchString(arg) {
			count = strings.TrimPrefix(arg, "-")
			continue
		}
		kept = append(kept, arg)
	}
	return kept, count
}

func runLog(ctx context.Context, env Env, args []string) (string, error) {
	args, shortCount := splitShortCount(args)
	opts, err := parseOptions(args, logOptions)
	if err != nil {
		return "", err
	}
	maxCount, err := countValue(opts, "max-count", shortCount)
	if err != nil {
		return "", err
	}
	skip, err := countValue(opts, "skip", "")
	if err != nil {
		return "", err
	}
	db, err := env.openObjects()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	store, err := env.openRefs()
	if err != nil {
		return "", err
	}
	defer func() { _ = store.Close() }()
	graph, err := ops.OpenCommitGraph(env.Repo, db)
	if err != nil {
		return "", err
	}
	source := revision.Context{Objects: db, Refs: store, Graph: graph}
	specs := opts.args()
	if len(specs) == 0 {
		specs = []string{string(refs.HEAD)}
	}
	walk, err := revision.Ranges(specs, source)
	if err != nil {
		return "", err
	}
	walk.MaxCount = maxCount
	walk.Skip = skip
	walk.Reverse = opts.has("reverse")
	walk.FirstParent = opts.has("first-parent")
	return collectLog(ctx, walk, opts.has("oneline"))
}

func countValue(opts options, long, fallback string) (int, error) {
	text := fallback
	if opts.has(long) {
		text = opts.value(long)
	}
	if text == "" {
		return 0, nil
	}
	number, err := strconv.Atoi(text)
	if err != nil || number < 0 {
		return 0, detail(ErrUsage, text)
	}
	return number, nil
}

func collectLog(ctx context.Context, walk revision.Options, oneline bool) (string, error) {
	var out []string
	for commit, err := range revision.Walk(ctx, walk) {
		if err != nil {
			return "", err
		}
		if oneline {
			out = append(out, onelineEntry(commit))
			continue
		}
		out = append(out, longEntry(commit))
	}
	if oneline {
		return strings.Join(out, "\n"), nil
	}
	return strings.Join(out, "\n\n"), nil
}

func onelineEntry(commit *revision.Commit) string {
	return commit.ID.String()[:shortHashLength] + " " + subjectOf(commit.Message)
}

func longEntry(commit *revision.Commit) string {
	lines := []string{
		"commit " + commit.ID.String(),
	}
	if len(commit.Parents) > 1 {
		lines = append(lines, "Merge: "+shortParents(commit))
	}
	lines = append(lines,
		"Author: "+signatureLabel(commit.Author),
		"Date:   "+commit.Author.When.Format(gitDateLayout),
		"",
	)
	for _, line := range strings.Split(strings.TrimRight(commit.Message, "\n"), "\n") {
		lines = append(lines, messageIndent+line)
	}
	return strings.Join(lines, "\n")
}

func shortParents(commit *revision.Commit) string {
	parts := make([]string, 0, len(commit.Parents))
	for _, parent := range commit.Parents {
		parts = append(parts, parent.String()[:shortHashLength])
	}
	return strings.Join(parts, " ")
}

func signatureLabel(sig object.Signature) string {
	return sig.Name + " <" + sig.Email + ">"
}

func subjectOf(message string) string {
	subject, _, _ := strings.Cut(strings.TrimLeft(message, "\n"), "\n")
	return strings.TrimSpace(subject)
}
