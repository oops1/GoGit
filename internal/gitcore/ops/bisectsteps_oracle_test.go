//go:build oracle

package ops

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

const bisectOracleEpoch = 1700000000

func bisectOracleAt(o *oracle, when int64) *oracle {
	stamp := strconv.FormatInt(when, 10) + " +0000"
	env := append(slices.Clone(o.env),
		"GIT_AUTHOR_NAME=oracle", "GIT_AUTHOR_EMAIL=oracle@example.com", "GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_NAME=oracle", "GIT_COMMITTER_EMAIL=oracle@example.com", "GIT_COMMITTER_DATE="+stamp)
	return &oracle{t: o.t, home: o.home, env: env}
}

type bisectOracleBuilder struct {
	o    *oracle
	dir  string
	when int64
}

func (b *bisectOracleBuilder) at() *oracle {
	b.when++
	return bisectOracleAt(b.o, bisectOracleEpoch+b.when*60)
}

func (b *bisectOracleBuilder) commit(name, message, text string) {
	at := b.at()
	at.write(b.dir, name, text)
	at.run(b.dir, "add", name)
	at.run(b.dir, "commit", "-q", "-m", message)
}

func (b *bisectOracleBuilder) git(args ...string) { b.at().run(b.dir, args...) }

func bisectOracleChain(b *bisectOracleBuilder, first, commits int) {
	for i := first; i < first+commits; i++ {
		b.commit("f", "c"+strconv.Itoa(i), strconv.Itoa(i)+"\n")
	}
}

func bisectOracleBranchy(b *bisectOracleBuilder) {
	bisectOracleChain(b, 0, 3)
	b.git("switch", "-q", "-c", "side")
	for i := range 3 {
		b.commit("s", "s"+strconv.Itoa(i), strconv.Itoa(i)+"\n")
	}
	b.git("switch", "-q", "main")
	bisectOracleChain(b, 3, 2)
	b.git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	bisectOracleChain(b, 5, 2)
}

func bisectOracleForked(b *bisectOracleBuilder) {
	bisectOracleBranchy(b)
	b.git("switch", "-q", "-c", "other", "main~6")
	b.commit("o", "o0", "0\n")
	b.git("switch", "-q", "main")
}

func gitBisectLogsTheCommentsWeWrite(o *oracle) bool {
	o.t.Helper()
	dir := o.repoDir("bisect-format-probe")
	newOracleRepo(o, dir)
	bisectOracleChain(&bisectOracleBuilder{o: o, dir: dir}, 0, 3)
	o.run(dir, "bisect", "start")
	if o.read(dir, ".git/BISECT_LOG") != "git bisect start\n# status: waiting for both good and bad commits\n" {
		return false
	}
	o.run(dir, "bisect", "reset")
	bad := strings.TrimSpace(o.run(dir, "rev-parse", "main"))
	good := strings.TrimSpace(o.run(dir, "rev-parse", "main~2"))
	o.run(dir, "bisect", "start", "main", "main~2")
	head := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	o.run(dir, "bisect", "bad")
	return o.read(dir, ".git/BISECT_LOG") == "# bad: ["+bad+"] c2\n# good: ["+good+"] c0\n"+
		"git bisect start 'main' 'main~2'\n"+
		"# bad: ["+head+"] c1\ngit bisect bad "+head+"\n"+
		"# first bad commit: ["+head+"] c1\n"
}

type bisectSides struct {
	o        *oracle
	git      string
	ours     string
	repo     *repo.Repository
	comments bool
}

func bisectOracleSides(t *testing.T, build func(*bisectOracleBuilder)) *bisectSides {
	t.Helper()
	o := newOracle(t)
	sides := &bisectSides{o: o, comments: gitBisectLogsTheCommentsWeWrite(o)}
	for _, name := range []string{"git", "ours"} {
		dir := o.repoDir(name)
		newOracleRepo(o, dir)
		build(&bisectOracleBuilder{o: o, dir: dir})
		if sides.git == "" {
			sides.git = dir
			continue
		}
		sides.ours = dir
	}
	if got, want := strings.TrimSpace(o.run(sides.ours, "rev-parse", "HEAD")), strings.TrimSpace(o.run(sides.git, "rev-parse", "HEAD")); got != want {
		t.Fatalf("the two sides differ: %s and %s", got, want)
	}
	sides.repo = o.openRepo(sides.ours)
	return sides
}

func bisectOracleLinearSides(t *testing.T, commits int) *bisectSides {
	t.Helper()
	return bisectOracleSides(t, func(b *bisectOracleBuilder) { bisectOracleChain(b, 0, commits) })
}

func (s *bisectSides) state(dir string) string {
	s.o.t.Helper()
	text := bisectStateOf(s.o, dir)
	if s.comments {
		return text
	}
	head, log, _ := strings.Cut(text, bisectLogSection)
	var kept strings.Builder
	for line := range strings.Lines(log) {
		if !strings.HasPrefix(line, "# ") {
			kept.WriteString(line)
		}
	}
	return head + bisectLogSection + kept.String()
}

func (s *bisectSides) match(step string) {
	s.o.t.Helper()
	if got, want := s.state(s.ours), s.state(s.git); got != want {
		s.o.t.Fatalf("after %s\nours:\n%s\ngit:\n%s", step, got, want)
	}
}

func (s *bisectSides) gitBisect(args ...string) {
	s.o.t.Helper()
	_, _ = s.o.attempt(s.git, append([]string{"bisect"}, args...)...)
}

func TestOracleBisectStartGoodBadAndResetMatchGit(t *testing.T) {
	s := bisectOracleLinearSides(t, 10)

	s.gitBisect("start")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.match("start")

	s.gitBisect("bad")
	if _, err := MarkBisect(t.Context(), s.repo, BisectBad); err != nil {
		t.Fatalf("MarkBisect returned error %v", err)
	}
	s.match("bad")

	good := strings.TrimSpace(s.o.run(s.ours, "rev-parse", "main~5"))
	s.gitBisect("good", good)
	status, err := MarkBisectRevision(t.Context(), s.repo, BisectGood, good)
	if err != nil || status.Outcome != BisectTesting || status.Remaining != 2 || status.Steps != 1 {
		t.Fatalf("MarkBisectRevision = %+v, %v", status, err)
	}
	s.match("good")

	for _, term := range []string{"bad", "good"} {
		s.gitBisect(term)
		if status, err = MarkBisect(t.Context(), s.repo, BisectMark(term)); err != nil {
			t.Fatalf("MarkBisect %s returned error %v", term, err)
		}
		s.match(term)
	}
	if status.Outcome != BisectFound {
		t.Fatalf("the bisect ended as %+v, want the first bad commit", status)
	}

	s.gitBisect("reset")
	if err := ResetBisect(t.Context(), s.repo); err != nil {
		t.Fatalf("ResetBisect returned error %v", err)
	}
	s.match("reset")
}

func TestOracleBisectWithRevisionsAndSkipMatchesGit(t *testing.T) {
	s := bisectOracleLinearSides(t, 10)

	s.gitBisect("start", "main", "main~5")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"main~5"}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.match("start with revisions")

	var status BisectStatus
	for _, term := range []string{"skip", "bad", "good"} {
		s.gitBisect(term)
		var err error
		if status, err = MarkBisect(t.Context(), s.repo, BisectMark(term)); err != nil {
			t.Fatalf("MarkBisect %s returned error %v", term, err)
		}
		s.match(term)
	}
	if status.Outcome != BisectOnlySkipped || len(status.Candidates) != 2 {
		t.Fatalf("the bisect ended as %+v, want only skipped commits left", status)
	}
}

func TestOracleBisectOverMergesPicksTheSameRevisionsAsGit(t *testing.T) {
	s := bisectOracleSides(t, bisectOracleBranchy)

	s.gitBisect("start", "main", "main~7")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"main~7"}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.match("start")

	for step, term := range []string{"good", "bad", "good", "bad"} {
		s.gitBisect(term)
		status, err := MarkBisect(t.Context(), s.repo, BisectMark(term))
		if err != nil {
			t.Fatalf("MarkBisect %s returned error %v", term, err)
		}
		s.match(term + " " + strconv.Itoa(step))
		if status.Outcome != BisectTesting {
			break
		}
	}
}

func TestOracleBisectKeepsSkippingTheSameWayAsGit(t *testing.T) {
	s := bisectOracleLinearSides(t, 30)

	s.gitBisect("start", "main", "main~20")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"main~20"}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	for step := range 6 {
		s.gitBisect("skip")
		if _, err := MarkBisect(t.Context(), s.repo, BisectSkip); err != nil {
			t.Fatalf("MarkBisect skip returned error %v", err)
		}
		s.match("skip " + strconv.Itoa(step))
	}
}

func TestOracleBisectWithoutACheckoutMatchesGit(t *testing.T) {
	s := bisectOracleLinearSides(t, 10)

	s.gitBisect("start", "--no-checkout", "main", "main~5")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"main~5"}, NoCheckout: true}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.match("start without a checkout")

	s.gitBisect("bad")
	if _, err := MarkBisect(t.Context(), s.repo, BisectBad); err != nil {
		t.Fatalf("MarkBisect returned error %v", err)
	}
	s.match("bad without a checkout")
}

func TestOracleBisectFromADetachedHeadAndOverAMergeBaseMatchesGit(t *testing.T) {
	s := bisectOracleSides(t, bisectOracleForked)
	for _, dir := range []string{s.git, s.ours} {
		s.o.run(dir, "checkout", "-q", "main~1")
	}

	s.gitBisect("start", "main", "other")
	status, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"other"}})
	if err != nil || status.Outcome != BisectMergeBase {
		t.Fatalf("StartBisect = %+v, %v; want a merge base to test", status, err)
	}
	s.match("start over a merge base")

	s.gitBisect("good")
	if _, err := MarkBisect(t.Context(), s.repo, BisectGood); err != nil {
		t.Fatalf("MarkBisect returned error %v", err)
	}
	s.match("the merge base is good")

	s.gitBisect("reset")
	if err := ResetBisect(t.Context(), s.repo); err != nil {
		t.Fatalf("ResetBisect returned error %v", err)
	}
	s.match("reset to the detached head")
}

func TestOracleBisectRestartedOverAnotherBisectMatchesGit(t *testing.T) {
	s := bisectOracleLinearSides(t, 10)

	s.gitBisect("start", "main", "main~5")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main", Good: []string{"main~5"}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.gitBisect("start", "main~1", "main~4")
	if _, err := StartBisect(t.Context(), s.repo, BisectStartOptions{Bad: "main~1", Good: []string{"main~4"}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	s.match("a restarted bisect")
}
