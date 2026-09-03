package odb

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var errInjected = errors.New("odb: injected failure")

func TestLooseNameSplitsTheObjectName(t *testing.T) {
	id := parseID(t, "ce013625030ba8dba906f756967f9e9ca394464a")
	want := filepath.Join("ce", "013625030ba8dba906f756967f9e9ca394464a")
	if got := looseName(id); got != want {
		t.Fatalf("looseName gave %q, want %q", got, want)
	}
	if got := fanoutOf(id); got != "ce" {
		t.Fatalf("fanoutOf gave %q, want %q", got, "ce")
	}
}

func TestLooseIDRejectsForeignNames(t *testing.T) {
	cases := []struct {
		name   string
		fanout string
		entry  string
	}{
		{"short fanout", "c", "013625030ba8dba906f756967f9e9ca394464a"},
		{"short entry", "ce", "013625"},
		{"not hexadecimal", "zz", "013625030ba8dba906f756967f9e9ca394464a"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, ok := looseID(item.fanout, item.entry); ok {
				t.Fatal("looseID accepted a foreign name")
			}
		})
	}
}

func TestDecodeLooseHeaderRejectsMalformedHeaders(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		wanted error
	}{
		{"no terminator", "blob 5", object.ErrInvalidHeader},
		{"no space", "blob5\x00", object.ErrInvalidHeader},
		{"unknown type", "widget 5\x00", object.ErrUnknownType},
		{"size is not a number", "blob five\x00", object.ErrInvalidHeader},
		{"negative size", "blob -5\x00", object.ErrInvalidHeader},
		{"header without end", strings.Repeat("b", looseHeaderLimit+1), ErrHeaderTooLong},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			source := bufio.NewReader(strings.NewReader(item.raw))
			if _, _, err := decodeLooseHeader(source); !errors.Is(err, item.wanted) {
				t.Fatalf("decodeLooseHeader returned %v, want %v", err, item.wanted)
			}
		})
	}
}

func TestDecodeLooseRejectsBrokenStreams(t *testing.T) {
	if _, _, err := decodeLoose(bytes.NewReader([]byte("not compressed at all"))); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("decodeLoose returned %v, want %v", err, object.ErrMalformed)
	}
	if _, _, err := decodeLooseType(bytes.NewReader([]byte("not compressed at all"))); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("decodeLooseType returned %v, want %v", err, object.ErrMalformed)
	}
}

func TestDecodeLooseRejectsSizeMismatch(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"content is shorter than the header claims", "blob 9\x00short"},
		{"content is longer than the header claims", "blob 2\x00far too long"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			compressed := deflate(t, []byte(item.raw))
			if _, _, err := decodeLoose(bytes.NewReader(compressed)); !errors.Is(err, object.ErrSizeMismatch) {
				t.Fatalf("decodeLoose returned %v, want %v", err, object.ErrSizeMismatch)
			}
		})
	}
}

func TestDecodeLooseReportsTruncatedPayloads(t *testing.T) {
	compressed := deflate(t, []byte("blob 5\x00hello"))
	if _, _, err := decodeLoose(bytes.NewReader(compressed[:len(compressed)-4])); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("decodeLoose returned %v, want %v", err, object.ErrMalformed)
	}
}

func TestGetRejectsLooseObjectsWithADifferentName(t *testing.T) {
	objects := newObjectsDir(t)
	id := parseID(t, "ce013625030ba8dba906f756967f9e9ca394464a")
	writeLooseFile(t, objects, id, looseBytes(t, object.TypeBlob, []byte("different")))
	db := openDB(t, objects, Options{})
	if _, _, err := db.Get(id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get returned %v, want %v", err, ErrCorrupt)
	}
}

func TestGetReportsUnreadableLooseObjects(t *testing.T) {
	objects := newObjectsDir(t)
	id := parseID(t, "ce013625030ba8dba906f756967f9e9ca394464a")
	writeLooseFile(t, objects, id, []byte("this is not zlib"))
	db := openDB(t, objects, Options{})
	if _, _, err := db.Get(id); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Get returned %v, want %v", err, object.ErrMalformed)
	}
	if _, err := db.Type(id); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Type returned %v, want %v", err, object.ErrMalformed)
	}
}

func TestGetReportsLooseOpenFailures(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	swapRootOpen(t, always, errInjected)
	if _, _, err := db.Get(hash.Zero); !errors.Is(err, errInjected) {
		t.Fatalf("Get returned %v, want %v", err, errInjected)
	}
	if _, err := db.Size(hash.Zero); !errors.Is(err, errInjected) {
		t.Fatalf("Size returned %v, want %v", err, errInjected)
	}
}

func TestHasIgnoresDirectoriesNamedLikeObjects(t *testing.T) {
	objects := newObjectsDir(t)
	id := parseID(t, "ce013625030ba8dba906f756967f9e9ca394464a")
	if err := os.MkdirAll(filepath.Join(objects, looseName(id)), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	db := openDB(t, objects, Options{})
	known, err := db.Has(id)
	if err != nil || known {
		t.Fatalf("Has gave (%v, %v)", known, err)
	}
}

func TestHasReportsStatFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootStat(t, always, errInjected)
	if _, err := db.Has(hash.Zero); !errors.Is(err, errInjected) {
		t.Fatalf("Has returned %v, want %v", err, errInjected)
	}
}

func TestReadDirReportsFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if _, err := db.readDir("missing"); err == nil {
		t.Fatal("readDir accepted a missing directory")
	}
	id := parseID(t, "ce013625030ba8dba906f756967f9e9ca394464a")
	writeLooseFile(t, db.Dir(), id, looseBytes(t, object.TypeBlob, []byte("hello")))
	if _, err := db.readDir(looseName(id)); err == nil {
		t.Fatal("readDir accepted a regular file")
	}
}

func TestDecodeLooseRejectsBrokenHeaders(t *testing.T) {
	compressed := deflate(t, []byte("blob 5"))
	if _, _, err := decodeLoose(bytes.NewReader(compressed)); !errors.Is(err, object.ErrInvalidHeader) {
		t.Fatalf("decodeLoose returned %v, want %v", err, object.ErrInvalidHeader)
	}
}
