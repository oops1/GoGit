package diff

import (
	"bytes"
	"testing"
)

func fuzzOptions(algorithm, context uint8) Options {
	opts := Defaults()
	opts.Context = int(context % 8)
	opts.InterHunkContext = int(context % 3)
	if algorithm%2 == 1 {
		opts.Algorithm = AlgorithmHistogram
	}
	opts.IndentHeuristic = algorithm%4 < 2
	return opts
}

func FuzzBlobs(f *testing.F) {
	for _, pair := range corpus() {
		f.Add([]byte(pair.old), []byte(pair.new), uint8(0), uint8(3))
		f.Add([]byte(pair.old), []byte(pair.new), uint8(1), uint8(1))
	}
	f.Add([]byte("a\nb\nc"), []byte("a\nB\nc\n"), uint8(2), uint8(0))
	f.Add([]byte{0, 1, 2}, []byte{0, 1, 3}, uint8(3), uint8(7))

	f.Fuzz(func(t *testing.T, old, updated []byte, algorithm, context uint8) {
		opts := fuzzOptions(algorithm, context)
		hunks := Blobs(old, updated, opts)
		rebuilt, err := Apply(old, hunks)
		if err != nil {
			t.Fatalf("Apply returned error %v", err)
		}
		if !bytes.Equal(rebuilt, updated) {
			t.Fatalf("applying the hunks produced %q instead of %q", rebuilt, updated)
		}
		var buf bytes.Buffer
		file := File{
			OldPath: "old.txt", NewPath: "new.txt", Status: StatusModified,
			OldID: idOf(1), NewID: idOf(2), Hunks: hunks,
		}
		if err := Unified(&buf, file, opts); err != nil {
			t.Fatalf("Unified returned error %v", err)
		}
		if err := Stat(&buf, []File{file}, opts); err != nil {
			t.Fatalf("Stat returned error %v", err)
		}
		if err := NumStat(&buf, []File{file}); err != nil {
			t.Fatalf("NumStat returned error %v", err)
		}
	})
}

func FuzzInlineDiff(f *testing.F) {
	f.Add("alpha beta", "alpha gamma")
	f.Add("", "")
	f.Add("привет мир", "привет всем")
	f.Fuzz(func(t *testing.T, old, updated string) {
		spans := InlineDiff(old, updated)
		var kept, added bytes.Buffer
		for _, span := range spans {
			switch span.Kind {
			case KindContext:
				kept.WriteString(span.Text)
				added.WriteString(span.Text)
			case KindDel:
				kept.WriteString(span.Text)
			case KindAdd:
				added.WriteString(span.Text)
			}
		}
		if kept.String() != old {
			t.Fatalf("the spans rebuild %q instead of the old line %q", kept.String(), old)
		}
		if added.String() != updated {
			t.Fatalf("the spans rebuild %q instead of the new line %q", added.String(), updated)
		}
	})
}
