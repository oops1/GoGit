package diff

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type refusingWriter struct{}

var errRefused = errors.New("diff test: the writer refuses everything")

func (refusingWriter) Write([]byte) (int, error) { return 0, errRefused }

func textFile(path string, added, deleted int) File {
	file := File{
		OldPath: path,
		NewPath: path,
		OldMode: object.ModeBlob,
		NewMode: object.ModeBlob,
		OldID:   hash.ObjectID{1},
		NewID:   hash.ObjectID{2},
		Status:  StatusModified,
	}
	hunk := Hunk{OldStart: 1, OldLines: deleted, NewStart: 1, NewLines: added}
	for range deleted {
		hunk.Lines = append(hunk.Lines, Line{Kind: KindDel, Text: "old"})
	}
	for range added {
		hunk.Lines = append(hunk.Lines, Line{Kind: KindAdd, Text: "new"})
	}
	file.Hunks = []Hunk{hunk}
	return file
}

func statText(t *testing.T, files []File, opts Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Stat(&buf, files, opts); err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	return buf.String()
}

func TestStatWritesNothingWithoutInterestingFiles(t *testing.T) {
	unchanged := File{OldPath: "a.txt", NewPath: "a.txt", Status: StatusModified, OldMode: object.ModeBlob, NewMode: object.ModeBlob}
	if got := statText(t, []File{unchanged}, Defaults()); got != "" {
		t.Errorf("Stat wrote %q for a file without changes", got)
	}
	if got := statText(t, nil, Defaults()); got != "" {
		t.Errorf("Stat wrote %q for an empty list", got)
	}
	var buf bytes.Buffer
	if err := NumStat(&buf, []File{unchanged}); err != nil {
		t.Fatalf("NumStat returned error %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("NumStat wrote %q for a file without changes", buf.String())
	}
}

func TestStatSummaryUsesSingularAndPluralWords(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		want  string
	}{
		{"one insertion", []File{textFile("a.txt", 1, 0)}, " 1 file changed, 1 insertion(+)\n"},
		{"one deletion", []File{textFile("a.txt", 0, 1)}, " 1 file changed, 1 deletion(-)\n"},
		{
			"both sides",
			[]File{textFile("a.txt", 2, 3)},
			" 1 file changed, 2 insertions(+), 3 deletions(-)\n",
		},
		{
			"several files",
			[]File{textFile("a.txt", 1, 0), textFile("b.txt", 0, 1)},
			" 2 files changed, 1 insertion(+), 1 deletion(-)\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := statText(t, c.files, Defaults())
			if !strings.HasSuffix(got, c.want) {
				t.Errorf("Stat ended with %q instead of %q", got, c.want)
			}
		})
	}
}

func TestStatReportsWriteFailures(t *testing.T) {
	if err := Stat(refusingWriter{}, []File{textFile("a.txt", 1, 1)}, Defaults()); !errors.Is(err, errRefused) {
		t.Errorf("Stat returned %v instead of the writer error", err)
	}
	if err := NumStat(refusingWriter{}, []File{textFile("a.txt", 1, 1)}); !errors.Is(err, errRefused) {
		t.Errorf("NumStat returned %v instead of the writer error", err)
	}
	if err := Unified(refusingWriter{}, textFile("a.txt", 1, 1), Defaults()); !errors.Is(err, errRefused) {
		t.Errorf("Unified returned %v instead of the writer error", err)
	}
}

func TestShortenNameKeepsTheTailOfLongNames(t *testing.T) {
	cases := []struct {
		name          string
		value         string
		width         int
		wantPrefix    string
		wantShortened string
		wantPadding   int
	}{
		{"a short name is padded", "a.txt", 10, "", "a.txt", 5},
		{"an exact fit is not padded", "a.txt", 5, "", "a.txt", 0},
		{"a long name is cut at a slash", "one/two/three/four/five.txt", 12, "...", "/five.txt", 0},
		{"a long name without a slash keeps its tail", "abcdefghijklmnop", 8, "...", "lmnop", 0},
		{"a cut inside a directory keeps the padding", "one/twelvechars/x", 12, "...", "/x", 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prefix, shortened, padding := shortenName(c.value, c.width)
			if prefix != c.wantPrefix || shortened != c.wantShortened || padding != c.wantPadding {
				t.Errorf("shortenName(%q, %d) returned (%q, %q, %d) instead of (%q, %q, %d)",
					c.value, c.width, prefix, shortened, padding, c.wantPrefix, c.wantShortened, c.wantPadding)
			}
		})
	}
}

func TestDecimalWidthCountsTheDigits(t *testing.T) {
	cases := []struct {
		value int
		want  int
	}{{0, 1}, {9, 1}, {10, 2}, {999, 3}, {1000, 4}}
	for _, c := range cases {
		if got := decimalWidth(c.value); got != c.want {
			t.Errorf("decimalWidth(%d) returned %d instead of %d", c.value, got, c.want)
		}
	}
}

func TestScaleLinearKeepsZeroAtZero(t *testing.T) {
	if got := scaleLinear(0, 20, 100); got != 0 {
		t.Errorf("scaleLinear of zero returned %d", got)
	}
	if got := scaleLinear(100, 20, 100); got != 20 {
		t.Errorf("scaleLinear of the maximum returned %d instead of 20", got)
	}
}

func TestStatNarrowsTheGraphForLongNames(t *testing.T) {
	long := strings.Repeat("segment/", 9) + "file.txt"
	files := []File{textFile(long, 300, 300), textFile("short.txt", 1, 1)}
	got := statText(t, files, Defaults())
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if len(line) > DefaultStatWidth {
			t.Errorf("the stat line %q is %d columns wide", line, len(line))
		}
	}
	if !strings.Contains(got, "...") {
		t.Errorf("a long name was not shortened in %q", got)
	}
}

func TestStatWidthOptionChangesTheLayout(t *testing.T) {
	files := []File{textFile("some/directory/with/a/long/name.txt", 120, 40)}
	narrow := statText(t, files, withOptions(func(o *Options) { o.StatWidth = 40 }))
	wide := statText(t, files, withOptions(func(o *Options) { o.StatWidth = 200 }))
	if narrow == wide {
		t.Error("the stat width option did not change the output")
	}
	if strings.Count(wide, "+") <= strings.Count(narrow, "+") {
		t.Errorf("a wider layout drew %d plus signs and a narrow one %d", strings.Count(wide, "+"), strings.Count(narrow, "+"))
	}
}

func TestStatDrawsAtLeastOneSignPerSideOfASmallChange(t *testing.T) {
	files := []File{textFile("small.txt", 1, 1), textFile("big.txt", 400, 400)}
	got := statText(t, files, Defaults())
	line := strings.Split(got, "\n")[0]
	if !strings.Contains(line, "+") || !strings.Contains(line, "-") {
		t.Errorf("the smallest row %q lost one of its signs", line)
	}
}

func TestUninterestingSkipsOnlyUnchangedRegularFiles(t *testing.T) {
	unchanged := File{Status: StatusModified, OldMode: object.ModeBlob, NewMode: object.ModeBlob}
	if !uninteresting(unchanged, statRow{}) {
		t.Error("an unchanged file should be skipped")
	}
	if uninteresting(unchanged, statRow{binary: true}) {
		t.Error("a binary file should be kept")
	}
	if uninteresting(unchanged, statRow{added: 1}) {
		t.Error("a file with changes should be kept")
	}
	modeChanged := File{Status: StatusModified, OldMode: object.ModeBlob, NewMode: object.ModeExecutable}
	if uninteresting(modeChanged, statRow{}) {
		t.Error("a mode change should be kept")
	}
	if uninteresting(File{Status: StatusAdded}, statRow{}) {
		t.Error("an added file should be kept")
	}
}
