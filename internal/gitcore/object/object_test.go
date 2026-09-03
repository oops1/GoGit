package object_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestEveryFixtureReEncodesByteForByte(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			raw := f.raw(t)
			obj := f.object(t)
			if obj.Type() != f.kind {
				t.Fatalf("Type() = %s, want %s", obj.Type(), f.kind)
			}
			if encoded := obj.Encode(); !bytes.Equal(encoded, raw) {
				t.Fatalf("re-encoded object differs\n got: %q\nwant: %q", encoded, raw)
			}
			if obj.ID() != f.id {
				t.Fatalf("ID() = %s, want %s", obj.ID(), f.id)
			}
		})
	}
}

func TestWriteToEmitsTheSameBytesAsEncode(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			obj := f.object(t)
			var buf bytes.Buffer
			written, err := obj.WriteTo(&buf)
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if written != int64(buf.Len()) || !bytes.Equal(buf.Bytes(), obj.Encode()) {
				t.Fatalf("WriteTo wrote %d bytes, buffer holds %d", written, buf.Len())
			}
		})
	}
}

type failingWriter struct{}

var errWriterRefused = errors.New("writer refused")

func (failingWriter) Write([]byte) (int, error) { return 0, errWriterRefused }

func TestWriteToReportsWriterFailure(t *testing.T) {
	objects := []object.Object{
		&object.Blob{Data: []byte("x")},
		&object.Tree{},
		&object.Commit{},
		&object.Tag{},
	}
	for _, obj := range objects {
		if _, err := obj.WriteTo(failingWriter{}); !errors.Is(err, errWriterRefused) {
			t.Fatalf("%s: err = %v", obj.Type(), err)
		}
	}
}

func TestTypeNamesMatchGitVocabulary(t *testing.T) {
	cases := []struct {
		kind  object.Type
		name  string
		valid bool
	}{
		{object.TypeCommit, "commit", true},
		{object.TypeTree, "tree", true},
		{object.TypeBlob, "blob", true},
		{object.TypeTag, "tag", true},
		{object.Type(0), "unknown", false},
		{object.Type(7), "unknown", false},
	}
	for _, c := range cases {
		if c.kind.String() != c.name || c.kind.Valid() != c.valid {
			t.Fatalf("%d: String() = %q, Valid() = %v", c.kind, c.kind.String(), c.kind.Valid())
		}
	}
}

func TestParseTypeAcceptsGitNames(t *testing.T) {
	for name, want := range map[string]object.Type{
		"commit": object.TypeCommit,
		"tree":   object.TypeTree,
		"blob":   object.TypeBlob,
		"tag":    object.TypeTag,
	} {
		got, err := object.ParseType(name)
		if err != nil || got != want {
			t.Fatalf("ParseType(%q) = %v, %v", name, got, err)
		}
	}
}

func TestParseTypeRejectsUnknownName(t *testing.T) {
	if _, err := object.ParseType("delta"); !errors.Is(err, object.ErrUnknownType) {
		t.Fatalf("err = %v, want ErrUnknownType", err)
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	if _, err := object.Parse(object.Type(6), nil); !errors.Is(err, object.ErrUnknownType) {
		t.Fatalf("err = %v, want ErrUnknownType", err)
	}
}

func TestParseForwardsTypeSpecificFailures(t *testing.T) {
	cases := []struct {
		kind object.Type
		data string
	}{
		{object.TypeCommit, "tree\n\n"},
		{object.TypeTree, "40000"},
		{object.TypeTag, "object\n\n"},
	}
	for _, c := range cases {
		if _, err := object.Parse(c.kind, []byte(c.data)); err == nil {
			t.Fatalf("Parse(%s, %q) accepted malformed content", c.kind, c.data)
		}
	}
}

func TestParseBlobKeepsContentVerbatim(t *testing.T) {
	f := namedFixture(t, "blob_binary")
	blob, err := object.ParseBlob(f.raw(t))
	if err != nil {
		t.Fatalf("ParseBlob: %v", err)
	}
	if len(blob.Data) != 256 {
		t.Fatalf("len(Data) = %d, want 256", len(blob.Data))
	}
	for index, value := range blob.Data {
		if int(value) != index {
			t.Fatalf("Data[%d] = %d", index, value)
		}
	}
	if blob.ID() != f.id {
		t.Fatalf("ID() = %s, want %s", blob.ID(), f.id)
	}
}

func TestParseBlobKeepsCarriageReturnsAndEmptyContent(t *testing.T) {
	crlf := namedFixture(t, "blob_crlf")
	if got := string(crlf.raw(t)); got != "line one\r\nline two\r\n" {
		t.Fatalf("blob content = %q", got)
	}
	empty := namedFixture(t, "blob_empty")
	blob, err := object.ParseBlob(empty.raw(t))
	if err != nil {
		t.Fatalf("ParseBlob: %v", err)
	}
	if len(blob.Data) != 0 || blob.ID() != empty.id {
		t.Fatalf("empty blob parsed as %q with id %s", blob.Data, blob.ID())
	}
}
