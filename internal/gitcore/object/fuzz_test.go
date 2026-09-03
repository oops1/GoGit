package object_test

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func seedCorpus(f *testing.F, kind object.Type) {
	f.Helper()
	all, err := loadFixtures()
	if err != nil {
		f.Fatalf("load fixtures: %v", err)
	}
	for _, fixture := range all {
		if fixture.kind != kind {
			continue
		}
		raw, err := fixtureContent(fixture.path)
		if err != nil {
			f.Fatalf("seed %s: %v", fixture.name, err)
		}
		f.Add(raw)
	}
	f.Add([]byte(""))
	f.Add([]byte("\n\n"))
	f.Add([]byte(" \n\n"))
}

func checkReEncodeIsStable(t *testing.T, kind object.Type, data []byte) {
	t.Helper()
	parsed, err := object.Parse(kind, data)
	if err != nil {
		return
	}
	first := parsed.Encode()
	again, err := object.Parse(kind, first)
	if err != nil {
		t.Fatalf("re-encoded %s no longer parses: %v\nbytes: %q", kind, err, first)
	}
	if second := again.Encode(); !bytes.Equal(first, second) {
		t.Fatalf("encoding is not stable for %s\nfirst:  %q\nsecond: %q", kind, first, second)
	}
	if parsed.ID() != again.ID() {
		t.Fatalf("object id changed across re-encoding: %s and %s", parsed.ID(), again.ID())
	}
}

func FuzzParseCommit(f *testing.F) {
	seedCorpus(f, object.TypeCommit)
	f.Fuzz(func(t *testing.T, data []byte) {
		checkReEncodeIsStable(t, object.TypeCommit, data)
	})
}

func FuzzParseTree(f *testing.F) {
	seedCorpus(f, object.TypeTree)
	f.Fuzz(func(t *testing.T, data []byte) {
		checkReEncodeIsStable(t, object.TypeTree, data)
	})
}

func FuzzParseTag(f *testing.F) {
	seedCorpus(f, object.TypeTag)
	f.Fuzz(func(t *testing.T, data []byte) {
		checkReEncodeIsStable(t, object.TypeTag, data)
	})
}

func FuzzParseSignature(f *testing.F) {
	f.Add("A U Thor <author@example.com> 1700000000 +0300")
	f.Add("No Zone <nozone@example.com> 1700000000")
	f.Add(" <> 0 -0000")
	f.Fuzz(func(t *testing.T, line string) {
		signature, err := object.ParseSignature([]byte(line))
		if err != nil {
			return
		}
		if got := signature.String(); got != line {
			t.Fatalf("signature does not round trip\n got: %q\nwant: %q", got, line)
		}
	})
}

func FuzzDecodeRaw(f *testing.F) {
	f.Add([]byte("blob 6\x00hello\n"))
	f.Add([]byte("tree 0\x00"))
	f.Add([]byte("commit 0\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		parsed, err := object.DecodeRaw(data)
		if err != nil {
			return
		}
		again, err := object.DecodeRaw(object.EncodeLoose(parsed))
		if err != nil {
			t.Fatalf("a re-encoded loose object no longer decodes: %v", err)
		}
		if again.ID() != parsed.ID() {
			t.Fatalf("object id changed across re-encoding: %s and %s", parsed.ID(), again.ID())
		}
	})
}
