package ops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func bisectChain(t *testing.T, commits int) (*testRepo, []hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	ids := []hash.ObjectID{r.initialCommit()}
	for i := 1; i < commits; i++ {
		ids = append(ids, r.commitFiles("c"+strconv.Itoa(i), map[string]string{"f.txt": strconv.Itoa(i) + "\n"}))
	}
	return r, ids
}

func bisectForkedRepo(t *testing.T) (*testRepo, []hash.ObjectID, hash.ObjectID) {
	t.Helper()
	r, ids := bisectChain(t, 1)
	r.createBranch("side", ids[0])
	r.switchTo("side")
	side := r.commitFiles("s0", map[string]string{"s.txt": "s\n"})
	r.switchTo("main")
	for i := 1; i < 3; i++ {
		ids = append(ids, r.commitFiles("c"+strconv.Itoa(i), map[string]string{"f.txt": strconv.Itoa(i) + "\n"}))
	}
	return r, ids, side
}

func (r *testRepo) bisectRefNames() []string {
	r.t.Helper()
	store := r.refs()
	var names []string
	for ref, err := range store.Prefix(refs.BisectPrefix) {
		if err != nil {
			r.t.Fatalf("Prefix returned error %v", err)
		}
		names = append(names, ref.Name.String())
	}
	slices.Sort(names)
	return names
}

func (r *testRepo) bisectLog() string {
	r.t.Helper()
	text, err := BisectLog(r.repo)
	if err != nil {
		r.t.Fatalf("BisectLog returned error %v", err)
	}
	return text
}

func TestStartBisectWithoutRevisionsWaitsForBoth(t *testing.T) {
	r, _ := bisectChain(t, 3)

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{})

	if err != nil || status.Outcome != BisectPending || status.Start != "main" || status.HasBad {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if got := r.bisectLog(); got != "git bisect start\n# status: waiting for both good and bad commits\n" {
		t.Fatalf("log = %q", got)
	}
	if got := r.readFile(".git/BISECT_START"); got != "main\n" {
		t.Fatalf("BISECT_START = %q", got)
	}
	if got := r.readFile(".git/BISECT_NAMES"); got != "\n" {
		t.Fatalf("BISECT_NAMES = %q", got)
	}
	if r.exists(".git/BISECT_TERMS") {
		t.Fatal("BISECT_TERMS was written without a mark")
	}
}

func TestStartBisectNarrowsTheRangeAndFindsTheFirstBadCommit(t *testing.T) {
	r, ids := bisectChain(t, 10)

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[4].String()}})
	if err != nil || status.Outcome != BisectTesting || status.Current != ids[6] || status.Remaining != 2 || status.Steps != 1 {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if got, want := r.headCommit(r.refs()), ids[6]; got != want {
		t.Fatalf("HEAD = %s, want %s", got, want)
	}
	if got := r.readFile(".git/BISECT_TERMS"); got != "bad\ngood\n" {
		t.Fatalf("BISECT_TERMS = %q", got)
	}
	if got := r.readFile(".git/BISECT_EXPECTED_REV"); got != ids[6].String()+"\n" {
		t.Fatalf("BISECT_EXPECTED_REV = %q", got)
	}
	if got := r.readFile(".git/BISECT_ANCESTORS_OK"); got != "" {
		t.Fatalf("BISECT_ANCESTORS_OK = %q", got)
	}

	status, err = MarkBisect(t.Context(), r.repo, BisectBad)
	if err != nil || status.Outcome != BisectTesting || status.Current != ids[5] {
		t.Fatalf("MarkBisect bad = %+v, %v", status, err)
	}

	status, err = MarkBisect(t.Context(), r.repo, BisectGood)
	if err != nil || status.Outcome != BisectFound || status.Current != ids[6] || status.Subject != "c6" {
		t.Fatalf("MarkBisect good = %+v, %v", status, err)
	}
	if got, want := r.bisectLog(), "# first bad commit: ["+ids[6].String()+"] c6\n"; !strings.HasSuffix(got, want) {
		t.Fatalf("log = %q, want it to end with %q", got, want)
	}
}

func TestStartBisectLogsTheRevisionsItWasGiven(t *testing.T) {
	r, ids := bisectChain(t, 4)

	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[0].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	want := "# bad: [" + ids[3].String() + "] c3\n" +
		"# good: [" + ids[0].String() + "] initial\n" +
		"git bisect start 'main' '" + ids[0].String() + "'\n"
	if got := r.bisectLog(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	if got, want := r.bisectRefNames(), []string{"refs/bisect/bad", "refs/bisect/good-" + ids[0].String()}; !slices.Equal(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
}

func TestStartBisectRefusesGoodRevisionsWithoutABadOne(t *testing.T) {
	r, ids := bisectChain(t, 2)

	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Good: []string{ids[0].String()}}); !errors.Is(err, ErrBisectGoodWithoutBad) {
		t.Fatalf("StartBisect = %v, want ErrBisectGoodWithoutBad", err)
	}
}

func TestStartBisectReportsRevisionsItCannotResolve(t *testing.T) {
	tests := map[string]BisectStartOptions{
		"bad":  {Bad: "nowhere"},
		"good": {Bad: "main", Good: []string{"nowhere"}},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			r, _ := bisectChain(t, 2)

			if _, err := StartBisect(t.Context(), r.repo, opts); !errors.Is(err, ErrTargetNotFound) {
				t.Fatalf("StartBisect = %v, want ErrTargetNotFound", err)
			}
		})
	}
}

func TestStartBisectStopsOnACancelledContext(t *testing.T) {
	r, _ := bisectChain(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := StartBisect(ctx, r.repo, BisectStartOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("StartBisect = %v, want context.Canceled", err)
	}
}

func TestStartBisectOverAnotherBisectReturnsToTheOriginalBranchFirst(t *testing.T) {
	r, ids := bisectChain(t, 10)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[4].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: ids[8].String(), Good: []string{ids[5].String()}})

	if err != nil || status.Outcome != BisectTesting || status.Start != "main" {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if got := r.bisectLog(); strings.Count(got, "git bisect start") != 1 {
		t.Fatalf("log = %q, want a single start line", got)
	}
}

func TestMarkBisectWaitsUntilBothSidesAreKnown(t *testing.T) {
	r, ids := bisectChain(t, 4)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	for _, want := range []string{
		"# status: waiting for bad commit, 1 good commit known\n",
		"# status: waiting for bad commit, 2 good commits known\n",
	} {
		id := ids[0]
		if strings.Contains(want, "2 good") {
			id = ids[1]
		}
		status, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, id.String())
		if err != nil || status.Outcome != BisectPending {
			t.Fatalf("MarkBisectRevision = %+v, %v", status, err)
		}
		if got := r.bisectLog(); !strings.HasSuffix(got, want) {
			t.Fatalf("log = %q, want it to end with %q", got, want)
		}
	}
}

func TestMarkBisectRefusesWhenNoBisectIsInProgress(t *testing.T) {
	r, _ := bisectChain(t, 2)

	if _, err := MarkBisect(t.Context(), r.repo, BisectGood); !errors.Is(err, ErrNotBisecting) {
		t.Fatalf("MarkBisect = %v, want ErrNotBisecting", err)
	}
	if _, err := ReadBisectStatus(t.Context(), r.repo); !errors.Is(err, ErrNotBisecting) {
		t.Fatalf("ReadBisectStatus = %v, want ErrNotBisecting", err)
	}
	if _, err := BisectLog(r.repo); !errors.Is(err, ErrNotBisecting) {
		t.Fatalf("BisectLog = %v, want ErrNotBisecting", err)
	}
}

func TestMarkBisectAndReadBisectStatusStopOnACancelledContext(t *testing.T) {
	r, _ := bisectChain(t, 2)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := MarkBisect(ctx, r.repo, BisectGood); !errors.Is(err, context.Canceled) {
		t.Fatalf("MarkBisect = %v, want context.Canceled", err)
	}
	if _, err := ReadBisectStatus(ctx, r.repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadBisectStatus = %v, want context.Canceled", err)
	}
}

func TestMarkBisectSkipKeepsSearchingUntilOnlySkippedCommitsAreLeft(t *testing.T) {
	r, ids := bisectChain(t, 6)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[2].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := MarkBisect(t.Context(), r.repo, BisectSkip)
	if err != nil || status.Outcome != BisectTesting {
		t.Fatalf("MarkBisect skip = %+v, %v", status, err)
	}
	if got := r.readFile(".git/BISECT_TERMS"); got != "bad\ngood\n" {
		t.Fatalf("BISECT_TERMS = %q", got)
	}
	for status.Outcome == BisectTesting {
		if status, err = MarkBisect(t.Context(), r.repo, BisectSkip); err != nil {
			t.Fatalf("MarkBisect skip returned error %v", err)
		}
	}
	if status.Outcome != BisectOnlySkipped || len(status.Candidates) == 0 {
		t.Fatalf("the bisect ended as %+v", status)
	}
	log := r.bisectLog()
	if !strings.Contains(log, "# only skipped commits left to test\n") {
		t.Fatalf("log = %q", log)
	}
	for _, candidate := range status.Candidates {
		if !strings.Contains(log, "# possible first bad commit: ["+candidate.Commit.String()+"] "+candidate.Subject+"\n") {
			t.Fatalf("log = %q, missing %s", log, candidate.Commit)
		}
	}
}

func TestMarkBisectSkipOnACommitThatIsNotTheBestOneKeepsGoing(t *testing.T) {
	r, ids := bisectChain(t, 10)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[4].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := MarkBisectRevision(t.Context(), r.repo, BisectSkip, ids[8].String())

	if err != nil || status.Outcome != BisectTesting || status.Skipped != 1 {
		t.Fatalf("MarkBisectRevision = %+v, %v", status, err)
	}
	if status.Current == ids[8] || !slices.Contains(ids[5:8], status.Current) {
		t.Fatalf("testing %s, want a commit between the good one and the skipped one", status.Current)
	}
}

func TestMarkBisectSeesACommitThatIsBothGoodAndBad(t *testing.T) {
	r, ids := bisectChain(t, 4)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	if _, err := MarkBisect(t.Context(), r.repo, BisectBad); err != nil {
		t.Fatalf("MarkBisect bad returned error %v", err)
	}

	status, err := MarkBisect(t.Context(), r.repo, BisectGood)

	if err != nil || status.Outcome != BisectAmbiguous || status.Current != ids[3] {
		t.Fatalf("MarkBisect good = %+v, %v", status, err)
	}
}

func TestBisectWithoutACheckoutMovesOnlyItsOwnHead(t *testing.T) {
	r, ids := bisectChain(t, 10)

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[4].String()}, NoCheckout: true})

	if err != nil || status.Outcome != BisectTesting || status.Current != ids[6] {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if got, want := r.headCommit(r.refs()), ids[9]; got != want {
		t.Fatalf("HEAD = %s, want %s", got, want)
	}
	if got := r.readFile(".git/BISECT_HEAD"); got != ids[6].String()+"\n" {
		t.Fatalf("BISECT_HEAD = %q", got)
	}

	status, err = MarkBisect(t.Context(), r.repo, BisectBad)
	if err != nil || status.Current != ids[5] {
		t.Fatalf("MarkBisect = %+v, %v", status, err)
	}
	if got := r.readFile(".git/BISECT_HEAD"); got != ids[5].String()+"\n" {
		t.Fatalf("BISECT_HEAD = %q", got)
	}
}

func TestBisectTestsAMergeBaseWhenAGoodRevisionIsOnAnotherBranch(t *testing.T) {
	r, ids, side := bisectForkedRepo(t)

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{side.String()}})
	if err != nil || status.Outcome != BisectMergeBase || status.Current != ids[0] {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if r.exists(".git/BISECT_ANCESTORS_OK") {
		t.Fatal("the ancestors were marked checked while a merge base is untested")
	}

	status, err = ReadBisectStatus(t.Context(), r.repo)
	if err != nil || status.Outcome != BisectMergeBase || status.Current != ids[0] || status.Subject != "initial" {
		t.Fatalf("ReadBisectStatus = %+v, %v", status, err)
	}

	status, err = MarkBisect(t.Context(), r.repo, BisectGood)
	if err != nil || status.Outcome != BisectTesting || status.Current != ids[1] {
		t.Fatalf("MarkBisect = %+v, %v", status, err)
	}
	if !r.exists(".git/BISECT_ANCESTORS_OK") {
		t.Fatal("the ancestors were not marked checked")
	}
}

func TestBisectSkipsAMergeBaseThatIsAlreadyKnown(t *testing.T) {
	r, ids, side := bisectForkedRepo(t)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{side.String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := MarkBisect(t.Context(), r.repo, BisectSkip)

	if err != nil || status.Outcome != BisectTesting || status.Current != ids[1] {
		t.Fatalf("MarkBisect = %+v, %v", status, err)
	}
}

func TestBisectRefusesGoodRevisionsThatAreNotAncestorsOfTheBadOne(t *testing.T) {
	tests := map[string]struct {
		expected func(ids []hash.ObjectID) string
		want     error
	}{
		"mixed up":       {func([]hash.ObjectID) string { return "" }, ErrBisectGoodNotAncestor},
		"merge base bad": {func(ids []hash.ObjectID) string { return ids[1].String() + "\n" }, ErrBisectMergeBaseBad},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r, ids := bisectChain(t, 4)
			if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
				t.Fatalf("StartBisect returned error %v", err)
			}
			if _, err := MarkBisectRevision(t.Context(), r.repo, BisectBad, ids[1].String()); err != nil {
				t.Fatalf("MarkBisectRevision bad returned error %v", err)
			}
			if text := tc.expected(ids); text != "" {
				r.writeFile(".git/BISECT_EXPECTED_REV", text)
			}

			_, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, ids[3].String())

			if !errors.Is(err, tc.want) {
				t.Fatalf("MarkBisectRevision = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReadBisectStatusFollowsTheSearchWithoutChangingIt(t *testing.T) {
	r, ids := bisectChain(t, 10)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := ReadBisectStatus(t.Context(), r.repo)
	if err != nil || status.Outcome != BisectPending || status.Start != "main" {
		t.Fatalf("ReadBisectStatus = %+v, %v", status, err)
	}

	if _, err := MarkBisect(t.Context(), r.repo, BisectBad); err != nil {
		t.Fatalf("MarkBisect bad returned error %v", err)
	}
	if _, err := MarkBisectRevision(t.Context(), r.repo, BisectGood, ids[4].String()); err != nil {
		t.Fatalf("MarkBisectRevision good returned error %v", err)
	}
	before := r.bisectLog()

	status, err = ReadBisectStatus(t.Context(), r.repo)
	if err != nil || status.Outcome != BisectTesting || status.Current != ids[6] || status.Remaining != 2 || status.Steps != 1 {
		t.Fatalf("ReadBisectStatus = %+v, %v", status, err)
	}
	if status.Good != 1 || status.Skipped != 0 || !status.HasBad {
		t.Fatalf("ReadBisectStatus counted %+v", status)
	}
	if got := r.bisectLog(); got != before {
		t.Fatalf("ReadBisectStatus wrote to the log: %q", got)
	}
}

func TestReadBisectStatusReportsTheFirstBadCommitAndTheSkippedOnes(t *testing.T) {
	r, ids := bisectChain(t, 4)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[2].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}

	status, err := ReadBisectStatus(t.Context(), r.repo)
	if err != nil || status.Outcome != BisectFound || status.Current != ids[3] {
		t.Fatalf("ReadBisectStatus = %+v, %v", status, err)
	}

	if _, err := MarkBisectRevision(t.Context(), r.repo, BisectSkip, ids[3].String()); err != nil {
		t.Fatalf("MarkBisectRevision skip returned error %v", err)
	}

	status, err = ReadBisectStatus(t.Context(), r.repo)
	if err != nil || status.Outcome != BisectOnlySkipped || status.Current != ids[3] || len(status.Candidates) != 1 {
		t.Fatalf("ReadBisectStatus = %+v, %v", status, err)
	}
}

func TestBisectHandlesAMergeInTheSearchedRange(t *testing.T) {
	r, ids := bisectChain(t, 2)
	r.createBranch("side", ids[1])
	r.switchTo("side")
	side := r.commitFiles("s0", map[string]string{"s.txt": "s\n"})
	r.switchTo("main")
	r.commitFiles("c2", map[string]string{"f.txt": "2\n"})
	if _, err := Merge(t.Context(), r.repo, "side", MergeOptions{Mode: MergeNoFastForward}); err != nil {
		t.Fatalf("Merge returned error %v", err)
	}
	last := r.commitFiles("c3", map[string]string{"f.txt": "3\n"})

	status, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: last.String(), Good: []string{ids[0].String()}})

	if err != nil || status.Outcome != BisectTesting || status.Current != side {
		t.Fatalf("StartBisect = %+v, %v", status, err)
	}
	if status.Remaining != 2 || status.Steps != 1 {
		t.Fatalf("StartBisect reported %+v", status)
	}
}

func TestQuoteBisectArgsWrapsEveryArgumentTheWayGitDoes(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"main"}, " 'main'"},
		{[]string{"main", "main~5"}, " 'main' 'main~5'"},
		{[]string{"it's"}, " 'it'\\''s'"},
		{[]string{"a!b"}, " 'a'\\!'b'"},
	}
	for _, tc := range tests {
		if got := quoteBisectArgs(tc.args); got != tc.want {
			t.Fatalf("quoteBisectArgs(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestEstimateBisectStepsCountsTheRemainingHalvings(t *testing.T) {
	tests := map[int]int{0: 0, 1: 0, 2: 0, 3: 1, 4: 1, 5: 1, 6: 2, 7: 2, 8: 2, 9: 2, 16: 3, 17: 3, 100: 6}
	for all, want := range tests {
		if got := estimateBisectSteps(all); got != want {
			t.Fatalf("estimateBisectSteps(%d) = %d, want %d", all, got, want)
		}
	}
}

func TestIntegerRootIsTheFlooredSquareRoot(t *testing.T) {
	for value := -2; value < 200; value++ {
		want := 0
		for want*want <= value {
			want++
		}
		want = max(want-1, 0)
		if got := integerRoot(value); got != want {
			t.Fatalf("integerRoot(%d) = %d, want %d", value, got, want)
		}
	}
}

func bisectingChain(t *testing.T, commits, good int) (*testRepo, []hash.ObjectID) {
	t.Helper()
	r, ids := bisectChain(t, commits)
	if _, err := StartBisect(t.Context(), r.repo, BisectStartOptions{Bad: "main", Good: []string{ids[good].String()}}); err != nil {
		t.Fatalf("StartBisect returned error %v", err)
	}
	return r, ids
}

func failWriteOf(t *testing.T, target string) {
	t.Helper()
	swapSeam(t, &fsRootWriteFile, func(original func(*os.Root, string, []byte, fs.FileMode) error) func(*os.Root, string, []byte, fs.FileMode) error {
		return func(root *os.Root, name string, data []byte, mode fs.FileMode) error {
			if name == target {
				return errInjected
			}
			return original(root, name, data, mode)
		}
	})
}

func failAppendAfter(t *testing.T, target string, skip int) {
	t.Helper()
	seen := 0
	swapSeam(t, &fsRootOpenFile, func(original func(*os.Root, string, int, fs.FileMode) (*os.File, error)) func(*os.Root, string, int, fs.FileMode) (*os.File, error) {
		return func(root *os.Root, name string, flag int, mode fs.FileMode) (*os.File, error) {
			if name != target {
				return original(root, name, flag, mode)
			}
			seen++
			if seen > skip {
				return nil, errInjected
			}
			return original(root, name, flag, mode)
		}
	})
}

func failCommitOf(t *testing.T, target hash.ObjectID) {
	t.Helper()
	swapSeam(t, &dbCommit, func(original func(*odb.DB, hash.ObjectID) (*object.Commit, error)) func(*odb.DB, hash.ObjectID) (*object.Commit, error) {
		return func(db *odb.DB, id hash.ObjectID) (*object.Commit, error) {
			if id == target {
				return nil, errInjected
			}
			return original(db, id)
		}
	})
}

func failEveryObjectRead(t *testing.T) {
	t.Helper()
	swapSeam(t, &dbGet, func(func(*odb.DB, hash.ObjectID) (object.Type, []byte, error)) func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) {
		return func(*odb.DB, hash.ObjectID) (object.Type, []byte, error) { return 0, nil, errInjected }
	})
}
