package object_test

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func loosePath(dir string, id hash.ObjectID) string {
	name := id.String()
	return filepath.Join(dir, name[:2], name[2:])
}

func TestReadLooseAcceptsEveryObjectGitWrote(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			obj, err := object.ReadLoose(f.path)
			if err != nil {
				t.Fatalf("ReadLoose: %v", err)
			}
			if obj.Type() != f.kind || obj.ID() != f.id {
				t.Fatalf("read %s %s, want %s %s", obj.Type(), obj.ID(), f.kind, f.id)
			}
			if !bytes.Equal(obj.Encode(), f.raw(t)) {
				t.Fatal("content differs from the fixture")
			}
		})
	}
}

func TestWriteLooseReproducesTheFilesGitWrote(t *testing.T) {
	dir := t.TempDir()
	for _, f := range fixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			id, err := object.WriteLoose(dir, f.object(t))
			if err != nil {
				t.Fatalf("WriteLoose: %v", err)
			}
			if id != f.id {
				t.Fatalf("WriteLoose returned %s, want %s", id, f.id)
			}
			written, err := os.ReadFile(loosePath(dir, id))
			if err != nil {
				t.Fatalf("read written object: %v", err)
			}
			if !bytes.Equal(inflate(t, written), inflate(t, f.loose(t))) {
				t.Fatal("written object inflates to different bytes than the one git wrote")
			}
		})
	}
}

func inflate(t *testing.T, compressed []byte) []byte {
	t.Helper()
	decompressor, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("zlib: %v", err)
	}
	defer func() { _ = decompressor.Close() }()
	raw, err := io.ReadAll(decompressor)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	return raw
}

func TestWriteLooseMakesTheObjectReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows maps the unix mode onto the read-only attribute")
	}
	dir := t.TempDir()
	id, err := object.WriteLoose(dir, &object.Blob{Data: []byte("read only\n")})
	if err != nil {
		t.Fatalf("WriteLoose: %v", err)
	}
	info, err := os.Stat(loosePath(dir, id))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("mode = %v, want 0444", info.Mode().Perm())
	}
}

func TestWriteLooseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	blob := &object.Blob{Data: []byte("written twice\n")}
	first, err := object.WriteLoose(dir, blob)
	if err != nil {
		t.Fatalf("first WriteLoose: %v", err)
	}
	before, err := os.Stat(loosePath(dir, first))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	second, err := object.WriteLoose(dir, blob)
	if err != nil {
		t.Fatalf("second WriteLoose: %v", err)
	}
	if first != second {
		t.Fatalf("ids differ: %s and %s", first, second)
	}
	after, err := os.Stat(loosePath(dir, first))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("the existing object was rewritten")
	}
	entries, err := os.ReadDir(filepath.Join(dir, first.String()[:2]))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("fanout directory holds %d files, want 1", len(entries))
	}
}

func TestWriteLooseAndReadLooseAgree(t *testing.T) {
	dir := t.TempDir()
	blob := &object.Blob{Data: bytes.Repeat([]byte("round trip\n"), 1000)}
	id, err := object.WriteLoose(dir, blob)
	if err != nil {
		t.Fatalf("WriteLoose: %v", err)
	}
	back, err := object.ReadLoose(loosePath(dir, id))
	if err != nil {
		t.Fatalf("ReadLoose: %v", err)
	}
	if !bytes.Equal(back.Encode(), blob.Data) {
		t.Fatal("content changed on the way through the object store")
	}
}

func TestWriteLooseReportsAFanoutThatIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	blob := &object.Blob{Data: []byte("blocked\n")}
	fanout := filepath.Join(dir, blob.ID().String()[:2])
	if err := os.WriteFile(fanout, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := object.WriteLoose(dir, blob); err == nil {
		t.Fatal("WriteLoose accepted a fanout that is a regular file")
	}
}

func TestWriteLooseReportsADirectoryInPlaceOfTheObject(t *testing.T) {
	dir := t.TempDir()
	blob := &object.Blob{Data: []byte("blocked by a directory\n")}
	if err := os.MkdirAll(loosePath(dir, blob.ID()), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := object.WriteLoose(dir, blob); err == nil {
		t.Fatal("WriteLoose accepted a directory in place of the object file")
	}
	entries, err := os.ReadDir(filepath.Join(dir, blob.ID().String()[:2]))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the temporary file was left behind: %v", entries)
	}
}

func TestReadLooseReportsAMissingFile(t *testing.T) {
	missing := loosePath(t.TempDir(), namedFixture(t, "blob_hello").id)
	if _, err := object.ReadLoose(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadLooseRejectsPathsThatAreNotObjectNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"objects/ab/short", "objects/abc/" + strings.Repeat("0", 38), "objects/zz/" + strings.Repeat("0", 38)} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if _, err := object.ReadLoose(path); !errors.Is(err, object.ErrInvalidPath) {
			t.Fatalf("ReadLoose(%s) err = %v, want ErrInvalidPath", name, err)
		}
	}
}

func TestReadLooseRejectsContentThatDoesNotHashToItsName(t *testing.T) {
	dir := t.TempDir()
	id, err := object.WriteLoose(dir, &object.Blob{Data: []byte("original\n")})
	if err != nil {
		t.Fatalf("WriteLoose: %v", err)
	}
	path := loosePath(dir, id)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	var swapped bytes.Buffer
	compressor := zlib.NewWriter(&swapped)
	if _, err := compressor.Write([]byte("blob 9\x00different")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.WriteFile(path, swapped.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := object.ReadLoose(path); !errors.Is(err, object.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestReadLooseRejectsContentThatIsNotZlib(t *testing.T) {
	dir := t.TempDir()
	path := loosePath(dir, namedFixture(t, "blob_hello").id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("plain text"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := object.ReadLoose(path); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestDecodeLooseReportsTruncatedCompressedStreams(t *testing.T) {
	full := namedFixture(t, "blob_binary").loose(t)
	if _, err := object.DecodeLoose(bytes.NewReader(full[:len(full)-4])); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestDecodeRawRejectsBrokenHeaders(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"no nul", "blob 5 hello", object.ErrInvalidHeader},
		{"no space", "blob5\x00hello", object.ErrInvalidHeader},
		{"unknown type", "widget 5\x00hello", object.ErrUnknownType},
		{"size is not a number", "blob five\x00hello", object.ErrInvalidHeader},
		{"size too small", "blob 2\x00hello", object.ErrSizeMismatch},
		{"size too large", "blob 500\x00hello", object.ErrSizeMismatch},
		{"negative size", "blob -1\x00hello", object.ErrSizeMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := object.DecodeRaw([]byte(c.raw)); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestEncodeLooseWritesTheGitHeader(t *testing.T) {
	raw := object.EncodeLoose(&object.Blob{Data: []byte("hello\n")})
	if string(raw) != "blob 6\x00hello\n" {
		t.Fatalf("EncodeLoose = %q", raw)
	}
	back, err := object.DecodeRaw(raw)
	if err != nil {
		t.Fatalf("DecodeRaw: %v", err)
	}
	if back.ID().String() != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("ID = %s", back.ID())
	}
}

func TestIDFromLoosePathJoinsFanoutAndName(t *testing.T) {
	want := namedFixture(t, "blob_hello").id
	got, err := object.IDFromLoosePath(loosePath(filepath.Join("any", "objects"), want))
	if err != nil || got != want {
		t.Fatalf("IDFromLoosePath = %s, %v", got, err)
	}
}

func deflate(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	compressor := zlib.NewWriter(&out)
	if _, err := compressor.Write(raw); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return out.Bytes()
}

func TestReadLooseHeaderRejectsHeadersLongerThanTheLimit(t *testing.T) {
	source := bytes.NewReader([]byte(strings.Repeat("b", 65)))
	if _, _, err := object.ReadLooseHeader(source); !errors.Is(err, object.ErrHeaderTooLong) {
		t.Fatalf("err = %v, want ErrHeaderTooLong", err)
	}
}

func TestDecodeLooseRejectsAMalformedHeaderInsideAValidStream(t *testing.T) {
	compressed := deflate(t, []byte("blob five\x00hello"))
	if _, err := object.DecodeLoose(bytes.NewReader(compressed)); !errors.Is(err, object.ErrInvalidHeader) {
		t.Fatalf("err = %v, want ErrInvalidHeader", err)
	}
}

func TestDecodeLooseRejectsASizeMismatchInAnOtherwiseCompleteStream(t *testing.T) {
	compressed := deflate(t, []byte("blob 999\x00hello"))
	if _, err := object.DecodeLoose(bytes.NewReader(compressed)); !errors.Is(err, object.ErrSizeMismatch) {
		t.Fatalf("err = %v, want ErrSizeMismatch", err)
	}
}

func TestDecodeLooseDecodesACompleteStream(t *testing.T) {
	compressed := deflate(t, []byte("blob 5\x00hello"))
	obj, err := object.DecodeLoose(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("DecodeLoose: %v", err)
	}
	blob, ok := obj.(*object.Blob)
	if !ok || string(blob.Data) != "hello" {
		t.Fatalf("DecodeLoose gave %#v", obj)
	}
}

func TestWriteLooseReportsAMissingObjectsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	blob := &object.Blob{Data: []byte("no directory\n")}
	if _, err := object.WriteLoose(dir, blob); err == nil {
		t.Fatal("WriteLoose accepted a missing objects directory")
	}
}

func TestWriteLooseRawReportsTemporaryFileFailures(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	if err := root.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := object.WriteLooseRaw(root, object.TypeBlob, []byte("closed root\n")); err == nil {
		t.Fatal("WriteLooseRaw accepted a closed root")
	}
}
