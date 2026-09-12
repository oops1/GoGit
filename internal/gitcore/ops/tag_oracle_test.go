//go:build oracle

package ops

import (
	"strings"
	"testing"
	"time"
)

type tagScenario struct {
	name    string
	setup   func(b *mergeBuilder)
	tag     string
	target  string
	message string
	force   bool
	args    []string
	delete  string
}

func tagHistory(b *mergeBuilder) {
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f})
	b.commit("next", map[string]string{"f": editLine(f, 1, "NEXT")})
}

func taggedHistory(b *mergeBuilder) {
	tagHistory(b)
	b.git("tag", "-a", "-m", "first release", "v1")
	b.git("tag", "light", "HEAD~1")
}

func tagScenarios() []tagScenario {
	return []tagScenario{
		{name: "lightweight at head", setup: tagHistory, tag: "v1"},
		{name: "lightweight at an older commit", setup: tagHistory, tag: "v1", target: "HEAD~1"},
		{name: "annotated at head", setup: tagHistory, tag: "v1", message: "first release", args: []string{"-a", "-m", "first release"}},
		{name: "annotated with a body", setup: tagHistory, tag: "v1", message: "first release\n\nthe body of the tag\n", args: []string{"-a", "-m", "first release\n\nthe body of the tag"}},
		{name: "annotated at an older commit", setup: tagHistory, tag: "v1", target: "HEAD~1", message: "older", args: []string{"-a", "-m", "older"}},
		{name: "annotated over a tag object", setup: taggedHistory, tag: "v2", target: "v1", message: "on top of a tag", args: []string{"-a", "-m", "on top of a tag"}},
		{name: "lightweight over an existing name", setup: taggedHistory, tag: "light"},
		{name: "forced over an existing name", setup: taggedHistory, tag: "light", force: true, args: []string{"-f"}},
		{name: "forced annotated over a lightweight tag", setup: taggedHistory, tag: "light", force: true, message: "now annotated", args: []string{"-f", "-a", "-m", "now annotated"}},
		{name: "an invalid name", setup: tagHistory, tag: "bad..name"},
		{name: "an unknown target", setup: tagHistory, tag: "v1", target: "nope"},
		{name: "delete an annotated tag", setup: taggedHistory, delete: "v1"},
		{name: "delete a lightweight tag", setup: taggedHistory, delete: "light"},
		{name: "delete a tag that is not there", setup: taggedHistory, delete: "v9"},
	}
}

func tagStateOf(b *mergeBuilder) string {
	b.o.t.Helper()
	var out []string
	refs, _ := b.o.attempt(b.dir, "for-each-ref", "--format=%(refname) %(objecttype) %(*objectname)", "refs/tags")
	out = append(out, "== refs\n"+refs)
	for _, name := range strings.Fields(strings.ReplaceAll(refs, " ", "\n")) {
		if !strings.HasPrefix(name, "refs/tags/") {
			continue
		}
		content, _ := b.o.attempt(b.dir, "cat-file", "-p", name)
		out = append(out, "== object "+name+"\n"+content)
	}
	logs, _ := b.o.attempt(b.dir, "reflog", "--format=%gs", "refs/tags/v1")
	out = append(out, "== reflog\n"+logs)
	return strings.Join(out, "\n")
}

func TestOracleTagsMatchTheOnesGitWrites(t *testing.T) {
	for _, s := range tagScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			sides := [2]*mergeBuilder{}
			for i, name := range []string{"git", "ours"} {
				dir := o.repoDir(name)
				newOracleRepo(o, dir)
				sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
				s.setup(sides[i])
			}
			gitSide, ourSide := sides[0], sides[1]
			when := time.Unix(gitSide.clock+60, 0).UTC()

			gitErr := runTagByGit(gitSide, s, when)
			ourErr := runTagByUs(t, o, ourSide, s, when)
			if (gitErr != nil) != (ourErr != nil) {
				t.Fatalf("git failed = %v, ours = %v", gitErr, ourErr)
			}

			if got, want := tagStateOf(ourSide), tagStateOf(gitSide); got != want {
				t.Fatalf("tags differ from %s: %s", strings.TrimSpace(o.run(gitSide.dir, "--version")), sectionDiff(got, want))
			}
		})
	}
}

func runTagByGit(b *mergeBuilder, s tagScenario, when time.Time) error {
	b.o.t.Helper()
	stamp := when.Format("2006-01-02T15:04:05Z07:00")
	env := append([]string{"GIT_COMMITTER_DATE=" + stamp}, b.o.env...)
	runner := &oracle{t: b.o.t, home: b.o.home, env: env}
	if s.delete != "" {
		_, err := runner.attempt(b.dir, "tag", "-d", s.delete)
		return err
	}
	args := append([]string{"tag"}, s.args...)
	args = append(args, s.tag)
	if s.target != "" {
		args = append(args, s.target)
	}
	_, err := runner.attempt(b.dir, args...)
	return err
}

func runTagByUs(t *testing.T, o *oracle, b *mergeBuilder, s tagScenario, when time.Time) error {
	t.Helper()
	r := o.openRepo(b.dir)
	if s.delete != "" {
		return DeleteTag(t.Context(), r, s.delete)
	}
	_, err := CreateTag(t.Context(), r, s.tag, s.target, CreateTagOptions{Message: s.message, Force: s.force, When: when})
	return err
}
