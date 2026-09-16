package merge

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func renameBenchmarkBody(seed, lines int) string {
	var body strings.Builder
	for line := range lines {
		fmt.Fprintf(&body, "file %d line %d with some content to hash\n", seed, line)
	}
	return body.String()
}

func renameBenchmarkSides(b *testing.B, s *memoryStore, files int) (hash.ObjectID, hash.ObjectID, hash.ObjectID) {
	base, ours, theirs := Snapshot{}, Snapshot{}, Snapshot{}
	put := func(text string) Entry {
		id, err := s.Put(object.TypeBlob, []byte(text))
		if err != nil {
			b.Fatal(err)
		}
		return Entry{Mode: object.ModeBlob, ID: id}
	}
	for at := range files {
		text := renameBenchmarkBody(at, 120)
		moved := fmt.Sprintf("src/file%03d.txt", at)
		edited := fmt.Sprintf("kept/same%03d.txt", at)
		base[moved], base[edited] = put(text), put(text)
		ours[fmt.Sprintf("dst/moved%03d.txt", at)] = put(text + "ours\n")
		ours[edited] = put("ours " + text)
		theirs[fmt.Sprintf("other/moved%03d.txt", at)] = put(text + "theirs\n")
		theirs[edited] = put("theirs " + text)
	}
	ids := make([]hash.ObjectID, 3)
	for at, snapshot := range []Snapshot{base, ours, theirs} {
		id, err := snapshot.Write(s)
		if err != nil {
			b.Fatal(err)
		}
		ids[at] = id
	}
	return ids[0], ids[1], ids[2]
}

func BenchmarkDetectRenamesOnBothSides(b *testing.B) {
	s := newStore()
	base, ours, theirs := renameBenchmarkSides(b, s, 150)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DetectSideRenames(b.Context(), s, base, ours, theirs, RenameOptions{Limit: DefaultRenameLimit, DirectoryRenames: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDetectRenamesSkipsSourcesTheOtherSideLeftAlone(b *testing.B) {
	s := newStore()
	base, ours, _ := renameBenchmarkSides(b, s, 150)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DetectSideRenames(b.Context(), s, base, ours, base, RenameOptions{Limit: DefaultRenameLimit, DirectoryRenames: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDetectRenamesWithEveryPairingRelevant(b *testing.B) {
	s := newStore()
	base, ours, _ := renameBenchmarkSides(b, s, 150)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DetectRenames(b.Context(), s, base, ours); err != nil {
			b.Fatal(err)
		}
	}
}
