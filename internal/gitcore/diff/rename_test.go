package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func renameSummary(files []File) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, fmt.Sprintf("%s %s->%s %d", file.Status, file.OldPath, file.NewPath, file.Similarity))
	}
	slices.Sort(out)
	return out
}

func treeDiffOf(t *testing.T, old, updated treeFiles, opts Options) []File {
	t.Helper()
	store := newMemoryStore()
	files, err := Trees(t.Context(), store, buildTree(store, old), buildTree(store, updated), opts)
	if err != nil {
		t.Fatalf("Trees returned error %v", err)
	}
	return files
}

func TestRenameDetectionFollowsTheThreshold(t *testing.T) {
	body := poem("shared")
	halved := strings.Replace(body, "shared line", "changed line", 20)
	cases := []struct {
		name      string
		threshold int
		want      []string
	}{
		{"a low threshold accepts the pair", 10, []string{"R before.txt->after.txt 49"}},
		{"a high threshold rejects the pair", 90, []string{"A ->after.txt 0", "D before.txt->before.txt 0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := Defaults()
			opts.RenameThreshold = c.threshold
			files := treeDiffOf(t,
				treeFiles{"before.txt": blobSpec(body)},
				treeFiles{"after.txt": blobSpec(halved)},
				opts)
			for at := range files {
				if files[at].Status == StatusAdded {
					files[at].OldPath = ""
				}
			}
			if got := renameSummary(files); !slices.Equal(got, c.want) {
				t.Errorf("threshold %d produced %q instead of %q", c.threshold, got, c.want)
			}
		})
	}
}

func TestRenameLimitTurnsOffTheInexactSearch(t *testing.T) {
	old := treeFiles{}
	updated := treeFiles{}
	for at := range 6 {
		old[fmt.Sprintf("old%d.txt", at)] = blobSpec(poem(fmt.Sprintf("body %d", at)))
		updated[fmt.Sprintf("new%d.txt", at)] = blobSpec(poem(fmt.Sprintf("body %d", at)) + "tail\n")
	}
	opts := Defaults()
	opts.RenameLimit = 2
	for _, file := range treeDiffOf(t, old, updated, opts) {
		if file.Status == StatusRenamed {
			t.Fatalf("a rename was reported past the rename limit: %+v", file)
		}
	}
	opts.RenameLimit = 0
	renames := 0
	for _, file := range treeDiffOf(t, old, updated, opts) {
		if file.Status == StatusRenamed {
			renames++
		}
	}
	if renames != 6 {
		t.Errorf("an unlimited search found %d renames instead of six", renames)
	}
}

func TestCopyDetectionNeedsAModifiedSourceAndTheCopyOption(t *testing.T) {
	body := poem("shared")
	old := treeFiles{"source.txt": blobSpec(body)}
	updated := treeFiles{"source.txt": blobSpec(body + "tail\n"), "clone.txt": blobSpec(body)}

	want := []string{"A clone.txt->clone.txt 0", "M source.txt->source.txt 0"}
	if got := renameSummary(treeDiffOf(t, old, updated, Defaults())); !slices.Equal(got, want) {
		t.Errorf("without the copy option the diff is %q instead of %q", got, want)
	}

	opts := Defaults()
	opts.DetectCopies = true
	want = []string{"C source.txt->clone.txt 100", "M source.txt->source.txt 0"}
	if got := renameSummary(treeDiffOf(t, old, updated, opts)); !slices.Equal(got, want) {
		t.Errorf("with the copy option the diff is %q instead of %q", got, want)
	}

	unchangedSource := treeFiles{"source.txt": blobSpec(body), "clone.txt": blobSpec(body)}
	want = []string{"A clone.txt->clone.txt 0"}
	if got := renameSummary(treeDiffOf(t, old, unchangedSource, opts)); !slices.Equal(got, want) {
		t.Errorf("an unmodified source produced %q instead of %q", got, want)
	}
}

func TestExactRenamesPreferTheMatchingBasename(t *testing.T) {
	body := poem("shared")
	old := treeFiles{"one/report.txt": blobSpec(body), "two/other.txt": blobSpec(body)}
	updated := treeFiles{"three/report.txt": blobSpec(body), "four/other.txt": blobSpec(body)}
	want := []string{"R one/report.txt->three/report.txt 100", "R two/other.txt->four/other.txt 100"}
	if got := renameSummary(treeDiffOf(t, old, updated, Defaults())); !slices.Equal(got, want) {
		t.Errorf("the diff is %q instead of %q", got, want)
	}
}

func TestExactRenamesIgnoreSourcesWithADifferentMode(t *testing.T) {
	body := poem("shared")
	old := treeFiles{"before.txt": linkSpec(body)}
	updated := treeFiles{"after.txt": blobSpec(body)}
	for _, file := range treeDiffOf(t, old, updated, Defaults()) {
		if file.Status == StatusRenamed {
			t.Errorf("a symlink was renamed into a regular file: %+v", file)
		}
	}
}

func TestExactRenamesMatchSymlinksOfTheSameMode(t *testing.T) {
	old := treeFiles{"before": linkSpec("some/rather/long/target/path.txt")}
	updated := treeFiles{"after": linkSpec("some/rather/long/target/path.txt")}
	want := []string{"R before->after 100"}
	if got := renameSummary(treeDiffOf(t, old, updated, Defaults())); !slices.Equal(got, want) {
		t.Errorf("the diff is %q instead of %q", got, want)
	}
}

func TestBasenameMatchingSkipsAmbiguousNames(t *testing.T) {
	body := poem("shared")
	edited := strings.Replace(body, "shared line 1\n", "shared line one\n", 1)
	old := treeFiles{"one/report.txt": blobSpec(body), "two/report.txt": blobSpec(body + "extra\n")}
	updated := treeFiles{"three/report.txt": blobSpec(edited), "four/report.txt": blobSpec(edited + "extra\n")}
	files := treeDiffOf(t, old, updated, Defaults())
	renames := 0
	for _, file := range files {
		if file.Status == StatusRenamed {
			renames++
		}
	}
	if renames != 2 {
		t.Errorf("ambiguous basenames produced %d renames instead of two", renames)
	}
}

func TestRenameDetectionSkipsWhenOneSideIsEmpty(t *testing.T) {
	body := poem("shared")
	files := treeDiffOf(t,
		treeFiles{"a.txt": blobSpec(body)},
		treeFiles{"a.txt": blobSpec(body + "tail\n")},
		Defaults())
	if got := renameSummary(files); !slices.Equal(got, []string{"M a.txt->a.txt 0"}) {
		t.Errorf("a plain edit produced %q", got)
	}
}

func TestBasenameSameComparesTheTrailingNames(t *testing.T) {
	cases := []struct {
		old  string
		new  string
		want int
	}{
		{"a/report.txt", "b/report.txt", 1},
		{"report.txt", "report.txt", 1},
		{"report.txt", "b/report.txt", 1},
		{"a/report.txt", "report.txt", 1},
		{"a/one.txt", "b/two.txt", 0},
		{"long-report.txt", "report.txt", 0},
		{"report.txt", "long-report.txt", 0},
	}
	for _, c := range cases {
		if got := basenameSame(c.old, c.new); got != c.want {
			t.Errorf("basenameSame(%q, %q) returned %d instead of %d", c.old, c.new, got, c.want)
		}
	}
}

func TestBaseNameTakesTheLastSegment(t *testing.T) {
	if got := baseName("a/b/c.txt"); got != "c.txt" {
		t.Errorf("baseName returned %q instead of c.txt", got)
	}
	if got := baseName("c.txt"); got != "c.txt" {
		t.Errorf("baseName returned %q instead of c.txt", got)
	}
}

func TestCandidateCompareOrdersByScoreThenName(t *testing.T) {
	cases := []struct {
		name string
		a    candidate
		b    candidate
		want int
	}{
		{"two empty slots stay equal", candidate{dst: -1}, candidate{dst: -1}, 0},
		{"an empty slot sorts last", candidate{dst: -1}, candidate{dst: 1}, 1},
		{"a filled slot sorts first", candidate{dst: 1}, candidate{dst: -1}, -1},
		{"a higher score sorts first", candidate{dst: 1, score: 9}, candidate{dst: 1, score: 4}, -5},
		{"a matching basename breaks a tie", candidate{dst: 1, nameScore: 1}, candidate{dst: 1}, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := candidateCompare(c.a, c.b); got != c.want {
				t.Errorf("candidateCompare returned %d instead of %d", got, c.want)
			}
		})
	}
}

func TestEstimateSimilarityRejectsUnusableSides(t *testing.T) {
	regular := pair{
		file:    File{OldMode: object.ModeBlob, NewMode: object.ModeBlob},
		oldData: []byte(poem("a")),
		newData: []byte(poem("a")),
	}
	link := pair{file: File{OldMode: object.ModeSymlink, NewMode: object.ModeSymlink}}
	if got := estimateSimilarity(link, regular, 0); got != 0 {
		t.Errorf("a symlink source scored %d", got)
	}
	if got := estimateSimilarity(regular, link, 0); got != 0 {
		t.Errorf("a symlink destination scored %d", got)
	}
	empty := pair{file: File{OldMode: object.ModeBlob, NewMode: object.ModeBlob}}
	if got := estimateSimilarity(regular, empty, 0); got != 0 {
		t.Errorf("an empty destination scored %d", got)
	}
	small := pair{
		file:    File{OldMode: object.ModeBlob, NewMode: object.ModeBlob},
		newData: []byte("tiny\n"),
	}
	if got := estimateSimilarity(regular, small, int(maxScore)); got != 0 {
		t.Errorf("a size mismatch scored %d", got)
	}
	if got := estimateSimilarity(regular, regular, 0); got != int(maxScore) {
		t.Errorf("identical content scored %d instead of %d", got, int(maxScore))
	}
}

func TestCountChangesComparesTheContentSpans(t *testing.T) {
	copied, added := countChanges([]byte("alpha\nbeta\n"), []byte("alpha\nbeta\n"))
	if copied == 0 || added != 0 {
		t.Errorf("identical content reported %d copied and %d added bytes", copied, added)
	}
	copied, added = countChanges([]byte("alpha\n"), []byte("alpha\nbeta\n"))
	if copied == 0 || added == 0 {
		t.Errorf("an appended line reported %d copied and %d added bytes", copied, added)
	}
	copied, added = countChanges([]byte("alpha\nbeta\n"), []byte("alpha\n"))
	if copied == 0 || added != 0 {
		t.Errorf("a removed line reported %d copied and %d added bytes", copied, added)
	}
	copied, added = countChanges(nil, []byte("beta\n"))
	if copied != 0 || added == 0 {
		t.Errorf("an empty source reported %d copied and %d added bytes", copied, added)
	}
}

func TestHashCharsSkipsCarriageReturnsInText(t *testing.T) {
	withCRLF := hashChars([]byte("alpha\r\nbeta\r\n"))
	plain := hashChars([]byte("alpha\nbeta\n"))
	if len(withCRLF) != len(plain) {
		t.Fatalf("the CRLF text produced %d spans and the LF text %d", len(withCRLF), len(plain))
	}
	for at := range plain {
		if withCRLF[at] != plain[at] {
			t.Errorf("span %d differs between the CRLF and LF texts", at)
		}
	}
	long := make([]byte, 200)
	for at := range long {
		long[at] = byte('a' + at%26)
	}
	if len(hashChars(long)) == 0 {
		t.Error("a long line without newlines produced no spans")
	}
}

func TestCountChangesSplitsARepeatedSpan(t *testing.T) {
	copied, added := countChanges([]byte("alpha\n"), []byte("alpha\nalpha\n"))
	if copied != 6 || added != 6 {
		t.Errorf("a duplicated span reported %d copied and %d added bytes instead of 6 and 6", copied, added)
	}
}

func TestExactRenamesStopAfterTooManyEqualSources(t *testing.T) {
	body := poem("shared")
	old := treeFiles{}
	for at := range 130 {
		old[fmt.Sprintf("src%03d.txt", at)] = blobSpec(body)
	}
	updated := treeFiles{"dest.txt": blobSpec(body)}
	renames := 0
	for _, file := range treeDiffOf(t, old, updated, Defaults()) {
		if file.Status == StatusRenamed {
			renames++
		}
	}
	if renames != 1 {
		t.Errorf("a crowd of equal sources produced %d renames instead of one", renames)
	}
}

func TestBasenameMatchingRejectsUnrelatedContent(t *testing.T) {
	old := treeFiles{"one/report.txt": blobSpec(poem("first"))}
	updated := treeFiles{"two/report.txt": blobSpec("nothing alike at all\n")}
	for _, file := range treeDiffOf(t, old, updated, Defaults()) {
		if file.Status == StatusRenamed {
			t.Errorf("unrelated content with a shared basename was reported as a rename: %+v", file)
		}
	}
}

func TestInexactRenamesUseEachSourceOnlyOnce(t *testing.T) {
	body := poem("shared")
	old := treeFiles{"before.txt": blobSpec(body)}
	updated := treeFiles{
		"after-one.txt": blobSpec(body + "one\n"),
		"after-two.txt": blobSpec(body + "two\n"),
	}
	renames, adds := 0, 0
	for _, file := range treeDiffOf(t, old, updated, Defaults()) {
		switch file.Status {
		case StatusRenamed:
			renames++
		case StatusAdded:
			adds++
		}
	}
	if renames != 1 || adds != 1 {
		t.Errorf("one source matched %d renames and left %d additions instead of one each", renames, adds)
	}
}

func TestUniqueByBaseNameMarksRepeatedNames(t *testing.T) {
	paths := map[string]int{}
	uniqueByBaseName(paths, "report.txt", 3)
	if paths["report.txt"] != 3 {
		t.Errorf("the first name was recorded as %d instead of 3", paths["report.txt"])
	}
	uniqueByBaseName(paths, "report.txt", 5)
	if paths["report.txt"] != -1 {
		t.Errorf("a repeated name was recorded as %d instead of -1", paths["report.txt"])
	}
}
