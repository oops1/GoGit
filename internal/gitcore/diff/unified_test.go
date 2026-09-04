package diff

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func idOf(value byte) hash.ObjectID {
	var id hash.ObjectID
	for at := range id {
		id[at] = value
	}
	return id
}

func unifiedText(t *testing.T, file File, opts Options) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Unified(&buf, file, opts); err != nil {
		t.Fatalf("Unified returned error %v", err)
	}
	return buf.String()
}

func TestUnifiedWritesTheGitHeaders(t *testing.T) {
	oneLine := []Hunk{{
		OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
		Lines: []Line{{Kind: KindDel, Text: "old"}, {Kind: KindAdd, Text: "new"}},
	}}
	cases := []struct {
		name string
		file File
		want string
	}{
		{
			name: "an unchanged file writes nothing",
			file: File{OldPath: "a.txt", NewPath: "a.txt", OldMode: object.ModeBlob, NewMode: object.ModeBlob},
			want: "",
		},
		{
			name: "a modified file",
			file: File{
				OldPath: "a.txt", NewPath: "a.txt",
				OldMode: object.ModeBlob, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusModified, Hunks: oneLine,
			},
			want: "diff --git a/a.txt b/a.txt\n" +
				"index aaaaaaa..bbbbbbb 100644\n" +
				"--- a/a.txt\n+++ b/a.txt\n" +
				"@@ -1 +1 @@\n-old\n+new\n",
		},
		{
			name: "an added file",
			file: File{
				NewPath: "a.txt", NewMode: object.ModeBlob, NewID: idOf(0xbb), Status: StatusAdded,
				Hunks: []Hunk{{OldStart: 1, OldLines: 0, NewStart: 1, NewLines: 1, Lines: []Line{{Kind: KindAdd, Text: "new"}}}},
			},
			want: "diff --git a/a.txt b/a.txt\n" +
				"new file mode 100644\n" +
				"index 0000000..bbbbbbb\n" +
				"--- /dev/null\n+++ b/a.txt\n" +
				"@@ -0,0 +1 @@\n+new\n",
		},
		{
			name: "a deleted file",
			file: File{
				OldPath: "a.txt", OldMode: object.ModeBlob, OldID: idOf(0xaa), Status: StatusDeleted,
				Hunks: []Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 0, Lines: []Line{{Kind: KindDel, Text: "old"}}}},
			},
			want: "diff --git a/a.txt b/a.txt\n" +
				"deleted file mode 100644\n" +
				"index aaaaaaa..0000000\n" +
				"--- a/a.txt\n+++ /dev/null\n" +
				"@@ -1 +0,0 @@\n-old\n",
		},
		{
			name: "a mode change without content",
			file: File{
				OldPath: "a.txt", NewPath: "a.txt",
				OldMode: object.ModeBlob, NewMode: object.ModeExecutable,
				OldID: idOf(0xaa), NewID: idOf(0xaa), Status: StatusModified,
			},
			want: "diff --git a/a.txt b/a.txt\nold mode 100644\nnew mode 100755\n",
		},
		{
			name: "a rename",
			file: File{
				OldPath: "a.txt", NewPath: "b.txt",
				OldMode: object.ModeBlob, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xaa), Status: StatusRenamed, Similarity: 100,
			},
			want: "diff --git a/a.txt b/b.txt\nsimilarity index 100%\nrename from a.txt\nrename to b.txt\n",
		},
		{
			name: "a copy with an edit",
			file: File{
				OldPath: "a.txt", NewPath: "b.txt",
				OldMode: object.ModeBlob, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusCopied, Similarity: 80, Hunks: oneLine,
			},
			want: "diff --git a/a.txt b/b.txt\n" +
				"similarity index 80%\ncopy from a.txt\ncopy to b.txt\n" +
				"index aaaaaaa..bbbbbbb 100644\n" +
				"--- a/a.txt\n+++ b/b.txt\n" +
				"@@ -1 +1 @@\n-old\n+new\n",
		},
		{
			name: "a changed binary file",
			file: File{
				OldPath: "a.bin", NewPath: "a.bin",
				OldMode: object.ModeBlob, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusModified, Binary: true,
			},
			want: "diff --git a/a.bin b/a.bin\n" +
				"index aaaaaaa..bbbbbbb 100644\n" +
				"Binary files a/a.bin and b/a.bin differ\n",
		},
		{
			name: "a binary file that only changes mode",
			file: File{
				OldPath: "a.bin", NewPath: "a.bin",
				OldMode: object.ModeBlob, NewMode: object.ModeExecutable,
				OldID: idOf(0xaa), NewID: idOf(0xaa), Status: StatusModified, Binary: true,
			},
			want: "diff --git a/a.bin b/a.bin\nold mode 100644\nnew mode 100755\n",
		},
		{
			name: "a name with a space gains a tab",
			file: File{
				OldPath: "od d.txt", NewPath: "od d.txt",
				OldMode: object.ModeBlob, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusModified, Hunks: oneLine,
			},
			want: "diff --git a/od d.txt b/od d.txt\n" +
				"index aaaaaaa..bbbbbbb 100644\n" +
				"--- a/od d.txt\t\n+++ b/od d.txt\t\n" +
				"@@ -1 +1 @@\n-old\n+new\n",
		},
		{
			name: "a type change writes both parts",
			file: File{
				OldPath: "thing", NewPath: "thing",
				OldMode: object.ModeSymlink, NewMode: object.ModeBlob,
				OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusTypeChanged,
				Parts: []File{
					{
						OldPath: "thing", NewPath: "thing", OldMode: object.ModeSymlink, OldID: idOf(0xaa), Status: StatusDeleted,
						Hunks: []Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, Lines: []Line{{Kind: KindDel, Text: "target", NoNewline: true}}}},
					},
					{
						OldPath: "thing", NewPath: "thing", NewMode: object.ModeBlob, NewID: idOf(0xbb), Status: StatusAdded,
						Hunks: []Hunk{{OldStart: 1, NewStart: 1, NewLines: 1, Lines: []Line{{Kind: KindAdd, Text: "target"}}}},
					},
				},
			},
			want: "diff --git a/thing b/thing\n" +
				"deleted file mode 120000\n" +
				"index aaaaaaa..0000000\n" +
				"--- a/thing\n+++ /dev/null\n" +
				"@@ -1 +0,0 @@\n-target\n\\ No newline at end of file\n" +
				"diff --git a/thing b/thing\n" +
				"new file mode 100644\n" +
				"index 0000000..bbbbbbb\n" +
				"--- /dev/null\n+++ b/thing\n" +
				"@@ -0,0 +1 @@\n+target\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unifiedText(t, c.file, Defaults()); got != c.want {
				t.Errorf("Unified wrote\n%q\ninstead of\n%q", got, c.want)
			}
		})
	}
}

func TestUnifiedHonoursTheAbbreviationLength(t *testing.T) {
	file := File{
		OldPath: "a.txt", NewPath: "a.txt",
		OldMode: object.ModeBlob, NewMode: object.ModeBlob,
		OldID: idOf(0xaa), NewID: idOf(0xbb), Status: StatusModified,
		Hunks: []Hunk{{
			OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
			Lines: []Line{{Kind: KindDel, Text: "old"}, {Kind: KindAdd, Text: "new"}},
		}},
	}
	got := unifiedText(t, file, withOptions(func(o *Options) { o.Abbrev = 12 }))
	want := "diff --git a/a.txt b/a.txt\n" +
		"index aaaaaaaaaaaa..bbbbbbbbbbbb 100644\n" +
		"--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"
	if got != want {
		t.Errorf("Unified wrote %q instead of %q", got, want)
	}
}

func TestWriteRangeFollowsTheGitShorthand(t *testing.T) {
	cases := []struct {
		start int
		count int
		want  string
	}{
		{1, 1, "1"},
		{1, 0, "0,0"},
		{5, 3, "5,3"},
		{7, 0, "6,0"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		writeRange(&buf, c.start, c.count)
		if got := buf.String(); got != c.want {
			t.Errorf("writeRange(%d, %d) wrote %q instead of %q", c.start, c.count, got, c.want)
		}
	}
}

func TestHunkHeadersCarryTheEnclosingFunction(t *testing.T) {
	old := "func alpha() {\n\tone\n\ttwo\n\tthree\n\tfour\n\tfive\n\tsix\n\tseven\n}\n"
	updated := "func alpha() {\n\tone\n\ttwo\n\tthree\n\tfour\n\tfive\n\tsix\n\tchanged\n}\n"
	hunks := Blobs([]byte(old), []byte(updated), Defaults())
	if len(hunks) != 1 {
		t.Fatalf("the change produced %d hunks", len(hunks))
	}
	if hunks[0].Header != "func alpha() {" {
		t.Errorf("the hunk header is %q instead of the function line", hunks[0].Header)
	}
}

func TestFunctionRecordSkipsLinesThatCannotStartAName(t *testing.T) {
	cases := []struct {
		record string
		want   string
		ok     bool
	}{
		{"func alpha() {\n", "func alpha() {", true},
		{"_private() {\n", "_private() {", true},
		{"$shell() {\n", "$shell() {", true},
		{"\tindented\n", "", false},
		{"", "", false},
		{"123 not a name\n", "", false},
	}
	for _, c := range cases {
		got, ok := functionRecord(c.record)
		if got != c.want || ok != c.ok {
			t.Errorf("functionRecord(%q) returned (%q, %v) instead of (%q, %v)", c.record, got, ok, c.want, c.ok)
		}
	}
}

func TestFunctionRecordTrimsVeryLongLines(t *testing.T) {
	record := "func " + string(bytes.Repeat([]byte("x"), funcNameLimit)) + "\n"
	got, ok := functionRecord(record)
	if !ok || len(got) != funcNameLimit {
		t.Errorf("functionRecord returned a name of %d bytes and ok=%v", len(got), ok)
	}
}
