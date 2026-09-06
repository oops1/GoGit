package pack

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

func requireEmptyDir(t testing.TB, dir string) {
	t.Helper()
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) returned error %v", dir, err)
	}
	if len(left) != 0 {
		t.Fatalf("%q still holds %v after a failed IndexPack", dir, left)
	}
}

func TestIndexPackRoundTripsAPlainPack(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	ids = append(ids, source.add(object.TypeBlob, []byte("hello, world")))
	ids = append(ids, source.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n")))
	ids = append(ids, source.add(object.TypeCommit, []byte("tree deadbeef\nmessage\n")))
	ids = append(ids, source.add(object.TypeTag, []byte("object deadbeef\ntag v1\n")))

	var buf bytes.Buffer
	written, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), &buf, dir, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != len(ids) {
		t.Fatalf("Objects = %d, want %d", result.Objects, len(ids))
	}
	if result.Checksum != written.Checksum {
		t.Fatalf("Checksum = %s, want %s", result.Checksum, written.Checksum)
	}
	if result.Bytes != written.Bytes {
		t.Fatalf("Bytes = %d, want %d", result.Bytes, written.Bytes)
	}
	wantPack := filepath.Join(dir, "pack-"+result.Checksum.String()+packSuffix)
	wantIndex := filepath.Join(dir, "pack-"+result.Checksum.String()+indexSuffix)
	if result.PackPath != wantPack {
		t.Fatalf("PackPath = %q, want %q", result.PackPath, wantPack)
	}
	if result.IndexPath != wantIndex {
		t.Fatalf("IndexPath = %q, want %q", result.IndexPath, wantIndex)
	}

	store := openStore(t, dir)
	for _, id := range ids {
		wantKind, wantData, err := source.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", id, err)
		}
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("store.Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != wantKind || !bytes.Equal(data, wantData) {
			t.Errorf("store.Get(%s) did not reproduce the object", id)
		}
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestIndexPackResolvesOffsetDeltas(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	for i := range 8 {
		ids = append(ids, source.add(object.TypeBlob, similarBlob(i, 400)))
	}
	var buf bytes.Buffer
	if _, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{Window: 8, Depth: 8}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), &buf, dir, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != len(ids) {
		t.Fatalf("Objects = %d, want %d", result.Objects, len(ids))
	}

	store := openStore(t, dir)
	deltas := 0
	for _, id := range ids {
		wantKind, wantData, _ := source.Get(id)
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("store.Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != wantKind || !bytes.Equal(data, wantData) {
			t.Errorf("store.Get(%s) did not reproduce the object", id)
		}
	}
	for _, file := range store.Files() {
		for record := range file.Index.Objects() {
			offset, _ := file.Index.Find(record)
			head, err := file.Pack.HeaderAt(offset)
			if err != nil {
				t.Fatalf("HeaderAt(%d) returned error %v", offset, err)
			}
			if head.Kind == KindOffsetDelta {
				deltas++
			}
		}
	}
	if deltas == 0 {
		t.Fatal("the pack holds no offset deltas, the test did not exercise delta resolution")
	}
}

func TestIndexPackResolvesForwardReferencedRefDelta(t *testing.T) {
	baseData := []byte("chain base content that the deltas keep copying from")
	baseID := hash.SumSHA1(object.TypeBlob.String(), baseData)
	delta := slices.Concat(deltaSizes(int64(len(baseData)), int64(len(baseData))+1),
		copyOp(0, uint32(len(baseData))), insertOp([]byte("!")))

	builder := newPackBuilder()
	builder.addRefDelta(t, baseID, delta)
	builder.addObject(t, KindBlob, baseData)
	raw := builder.bytes()

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != 2 {
		t.Fatalf("Objects = %d, want 2", result.Objects)
	}

	store := openStore(t, dir)
	targetID := hash.SumSHA1(object.TypeBlob.String(), append(slices.Clone(baseData), '!'))
	kind, data, ok, err := store.Get(targetID)
	if err != nil || !ok {
		t.Fatalf("store.Get(%s) returned (%v, %v)", targetID, ok, err)
	}
	if kind != object.TypeBlob || string(data) != string(baseData)+"!" {
		t.Fatalf("store.Get(%s) gave %s %q", targetID, kind, data)
	}
	if _, _, ok, err := store.Get(baseID); err != nil || !ok {
		t.Fatalf("store.Get(%s) returned (%v, %v)", baseID, ok, err)
	}
}

func TestIndexPackFixesThinPackByAppendingMissingBases(t *testing.T) {
	source := newFakeSource()
	base := similarBlob(0, 400)
	baseID := source.add(object.TypeBlob, base)
	target := append(bytes.Clone(base), []byte("extra tail data appended to the thin target\n")...)
	targetID := source.add(object.TypeBlob, target)

	var buf bytes.Buffer
	if _, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{targetID},
		WriteOptions{Window: 4, Depth: 4, Thin: map[hash.ObjectID]struct{}{baseID: {}}}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	resolver := mapResolver{baseID: {kind: object.TypeBlob, data: base}}
	dir := t.TempDir()
	result, err := IndexPack(t.Context(), &buf, dir, IndexOptions{Bases: resolver, FixThin: true})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != 2 {
		t.Fatalf("Objects = %d, want 2 (the target plus the appended base)", result.Objects)
	}

	packfile, err := OpenPack(result.PackPath)
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	defer func() { _ = packfile.Close() }()
	if packfile.Count() != 2 {
		t.Fatalf("Count = %d, want 2", packfile.Count())
	}
	if err := packfile.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	if packfile.Checksum() != result.Checksum {
		t.Fatalf("Checksum = %s, the packfile trailer holds %s", result.Checksum, packfile.Checksum())
	}

	store := openStore(t, dir)
	kind, data, ok, err := store.Get(targetID)
	if err != nil || !ok {
		t.Fatalf("store.Get(%s) returned (%v, %v)", targetID, ok, err)
	}
	if kind != object.TypeBlob || !bytes.Equal(data, target) {
		t.Fatalf("store.Get(%s) did not reproduce the thin target", targetID)
	}
	kind, data, ok, err = store.Get(baseID)
	if err != nil || !ok {
		t.Fatalf("store.Get(%s) returned (%v, %v)", baseID, ok, err)
	}
	if kind != object.TypeBlob || !bytes.Equal(data, base) {
		t.Fatalf("store.Get(%s) did not reproduce the appended base", baseID)
	}
}

func TestIndexPackWithoutFixThinLeavesThePackThin(t *testing.T) {
	source := newFakeSource()
	base := similarBlob(0, 400)
	baseID := source.add(object.TypeBlob, base)
	target := append(bytes.Clone(base), []byte("extra tail data appended to the thin target\n")...)
	targetID := source.add(object.TypeBlob, target)

	var buf bytes.Buffer
	if _, err := WritePack(t.Context(), &buf, source, []hash.ObjectID{targetID},
		WriteOptions{Window: 4, Depth: 4, Thin: map[hash.ObjectID]struct{}{baseID: {}}}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	resolver := mapResolver{baseID: {kind: object.TypeBlob, data: base}}
	dir := t.TempDir()
	result, err := IndexPack(t.Context(), &buf, dir, IndexOptions{Bases: resolver, FixThin: false})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != 1 {
		t.Fatalf("Objects = %d, want 1 (the pack stays thin)", result.Objects)
	}

	index, err := OpenIndex(result.IndexPath)
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	defer func() { _ = index.Close() }()
	if _, ok, _ := index.Lookup(targetID); !ok {
		t.Fatal("the index does not list the resolved target")
	}
	if _, ok, _ := index.Lookup(baseID); ok {
		t.Fatal("the index lists the external base even though FixThin was false")
	}

	packfile, err := OpenPack(result.PackPath)
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	defer func() { _ = packfile.Close() }()
	offset, ok, err := index.Lookup(targetID)
	if err != nil || !ok {
		t.Fatalf("Lookup(%s) returned (%v, %v)", targetID, ok, err)
	}
	if _, _, err := packfile.ObjectAt(offset); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("ObjectAt returned %v, want %v (the base was not written into the pack)", err, ErrBaseNotFound)
	}
}

func TestIndexPackFixesRealThinPackFixture(t *testing.T) {
	raw := readFixture(t, thinPackPath)
	store := openFixtureStore(t)
	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{Bases: store, FixThin: true}); err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}

	rebuilt := openStore(t, dir)
	seen := 0
	for id := range rebuilt.Objects() {
		if _, _, ok, err := rebuilt.Get(id); err != nil || !ok {
			t.Fatalf("Get(%s) returned (%v, %v)", id, ok, err)
		}
		seen++
	}
	if seen == 0 {
		t.Fatal("the rebuilt store holds no objects")
	}
	if err := rebuilt.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

type failingResolver struct{ err error }

func (f failingResolver) ResolveBase(hash.ObjectID, int) (object.Type, []byte, error) {
	return 0, nil, f.err
}

func TestIndexPackPropagatesBaseResolverErrors(t *testing.T) {
	raw := readFixture(t, thinPackPath)
	wantErr := errors.New("boom from the base resolver")
	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{Bases: failingResolver{err: wantErr}}); !errors.Is(err, wantErr) {
		t.Fatalf("IndexPack returned %v, want wrapping %v", err, wantErr)
	}
	requireEmptyDir(t, dir)
}

func TestWriteIndexFileReportsCreateFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if err := writeIndexFile(dir, filepath.Join(dir, "pack.idx"), nil, hash.Zero); err == nil {
		t.Fatal("writeIndexFile succeeded despite a missing directory")
	}
}

func TestIndexPackReportsMissingBaseWithoutAResolver(t *testing.T) {
	raw := readFixture(t, thinPackPath)
	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrBaseNotFound)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackRejectsBrokenHeaders(t *testing.T) {
	valid := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	badMagic := slices.Clone(valid)
	badMagic[0] = 'x'
	badVersion := slices.Clone(valid)
	binary.BigEndian.PutUint32(badVersion[4:], 3)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"tooShort", valid[:headerSize-1], ErrTruncated},
		{"badMagic", badMagic, ErrBadMagic},
		{"version3", badVersion, ErrUnsupportedVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := IndexPack(t.Context(), bytes.NewReader(tc.raw), dir, IndexOptions{}); !errors.Is(err, tc.want) {
				t.Fatalf("IndexPack returned %v, want %v", err, tc.want)
			}
			requireEmptyDir(t, dir)
		})
	}
}

func TestIndexPackRejectsTruncatedStreams(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base object"))
	builder.addOffsetDelta(t, base, slices.Concat(deltaSizes(11, 11), copyOp(0, 11)))
	full := builder.bytes()

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"noObjects", full[:headerSize], ErrTruncated},
		{"halfStream", full[:headerSize+1], ErrDecompress},
		{"halfTrailer", full[:len(full)-1], ErrTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := IndexPack(t.Context(), bytes.NewReader(tc.raw), dir, IndexOptions{}); !errors.Is(err, tc.want) {
				t.Fatalf("IndexPack returned %v, want %v", err, tc.want)
			}
			requireEmptyDir(t, dir)
		})
	}
}

func TestIndexPackDetectsChecksumMismatch(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content"))
	raw := builder.bytes()
	raw[len(raw)-1] ^= 0xff

	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrChecksumMismatch)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackReportsMissingDirectory(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content"))
	raw := builder.bytes()
	dir := filepath.Join(t.TempDir(), "absent")
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("IndexPack returned %v, want %v", err, os.ErrNotExist)
	}
}

func TestIndexPackReportsRenameFailure(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content for rename failure test"))
	raw := builder.bytes()
	checksum := hash.ObjectID(raw[len(raw)-hash.Size:])

	dir := t.TempDir()
	blocking := filepath.Join(dir, "pack-"+checksum.String()+packSuffix)
	if err := os.Mkdir(blocking, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); err == nil {
		t.Fatal("IndexPack succeeded despite a blocking directory at the destination pack path")
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	if len(left) != 1 || left[0].Name() != filepath.Base(blocking) {
		t.Fatalf("ReadDir returned %v, want only the blocking directory", left)
	}
}

func TestIndexPackReportsIndexRenameFailure(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content for index rename failure test"))
	raw := builder.bytes()
	checksum := hash.ObjectID(raw[len(raw)-hash.Size:])

	dir := t.TempDir()
	blocking := filepath.Join(dir, "pack-"+checksum.String()+indexSuffix)
	if err := os.Mkdir(blocking, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{}); err == nil {
		t.Fatal("IndexPack succeeded despite a blocking directory at the destination index path")
	}
	packPath := filepath.Join(dir, "pack-"+checksum.String()+packSuffix)
	if _, err := os.Stat(packPath); err != nil {
		t.Fatalf("the pack file should remain after an index-write failure: %v", err)
	}
}

func TestIndexPackWritesAKeepFileWhenRequested(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("keep me"))
	raw := builder.bytes()

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{KeepName: "session-42"})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	keepPath := filepath.Join(dir, "pack-"+result.Checksum.String()+".keep")
	data, err := os.ReadFile(keepPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", keepPath, err)
	}
	if string(data) != "session-42" {
		t.Fatalf("the keep file holds %q, want %q", data, "session-42")
	}
}

func TestIndexPackReportsKeepWriteFailure(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content for keep failure test"))
	raw := builder.bytes()
	checksum := hash.ObjectID(raw[len(raw)-hash.Size:])

	dir := t.TempDir()
	blocking := filepath.Join(dir, "pack-"+checksum.String()+".keep")
	if err := os.Mkdir(blocking, 0o755); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	if _, err := IndexPack(t.Context(), bytes.NewReader(raw), dir, IndexOptions{KeepName: "x"}); err == nil {
		t.Fatal("IndexPack succeeded despite a blocking directory at the keep path")
	}
}

func TestIndexPackReportsProgress(t *testing.T) {
	source := newFakeSource()
	var ids []hash.ObjectID
	for i := range 6 {
		ids = append(ids, source.add(object.TypeBlob, similarBlob(i, 300)))
	}
	var buf bytes.Buffer
	if _, err := WritePack(t.Context(), &buf, source, ids, WriteOptions{Window: 6, Depth: 6}); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	var reports []progress.Report
	sink := progress.Func(func(r progress.Report) { reports = append(reports, r) })
	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), &buf, dir, IndexOptions{Progress: sink}); err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}

	var sawReceiving, sawResolving bool
	for _, r := range reports {
		switch r.Phase {
		case progress.PhaseReceiving:
			sawReceiving = true
		case progress.PhaseResolving:
			sawResolving = true
		}
	}
	if !sawReceiving {
		t.Error("no receiving progress was reported")
	}
	if !sawResolving {
		t.Error("no resolving progress was reported")
	}
}

type cancelAfterReader struct {
	source io.Reader
	cancel context.CancelFunc
	after  int
	read   int
}

func (c *cancelAfterReader) Read(into []byte) (int, error) {
	if len(into) > 1 {
		into = into[:1]
	}
	n, err := c.source.Read(into)
	c.read += n
	if c.read >= c.after {
		c.cancel()
	}
	return n, err
}

func TestIndexPackStopsDuringReceiveWhenContextIsCanceled(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("first object"))
	builder.addObject(t, KindBlob, []byte("second object"))
	full := builder.bytes()

	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelAfterReader{source: bytes.NewReader(full), cancel: cancel, after: headerSize + 1}
	dir := t.TempDir()
	if _, err := IndexPack(ctx, reader, dir, IndexOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("IndexPack returned %v, want %v", err, context.Canceled)
	}
	requireEmptyDir(t, dir)
}

func TestIndexPackStopsWhileResolvingDeltasWhenContextIsCanceled(t *testing.T) {
	baseData := []byte("chain base content that the deltas keep copying from")
	baseID := hash.SumSHA1(object.TypeBlob.String(), baseData)
	delta := slices.Concat(deltaSizes(int64(len(baseData)), int64(len(baseData))+1),
		copyOp(0, uint32(len(baseData))), insertOp([]byte("!")))
	builder := newPackBuilder()
	builder.addRefDelta(t, baseID, delta)
	builder.addObject(t, KindBlob, baseData)
	full := builder.bytes()

	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelAfterReader{source: bytes.NewReader(full), cancel: cancel, after: len(full)}
	dir := t.TempDir()
	if _, err := IndexPack(ctx, reader, dir, IndexOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("IndexPack returned %v, want %v", err, context.Canceled)
	}
	requireEmptyDir(t, dir)
}

type faultyAppendFile struct {
	*os.File
	failTruncate bool
	failSeek     bool
	failWrite    bool
	failWriteAt  bool
	failReadAt   bool
}

var errAppendFault = errors.New("indexpack_test: injected append failure")

func (f *faultyAppendFile) Truncate(size int64) error {
	if f.failTruncate {
		return errAppendFault
	}
	return f.File.Truncate(size)
}

func (f *faultyAppendFile) Seek(offset int64, whence int) (int64, error) {
	if f.failSeek {
		return 0, errAppendFault
	}
	return f.File.Seek(offset, whence)
}

func (f *faultyAppendFile) Write(p []byte) (int, error) {
	if f.failWrite {
		return 0, errAppendFault
	}
	return f.File.Write(p)
}

func (f *faultyAppendFile) WriteAt(p []byte, off int64) (int, error) {
	if f.failWriteAt {
		return 0, errAppendFault
	}
	return f.File.WriteAt(p, off)
}

func (f *faultyAppendFile) ReadAt(p []byte, off int64) (int, error) {
	if f.failReadAt {
		return 0, errAppendFault
	}
	return f.File.ReadAt(p, off)
}

func newAppendFaultFile(t *testing.T, content []byte) *faultyAppendFile {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "append-fault-*")
	if err != nil {
		t.Fatalf("CreateTemp returned error %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write(content); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	return &faultyAppendFile{File: file}
}

func TestAppendMissingBasesReportsEveryFailure(t *testing.T) {
	base := appendedBase{kind: object.TypeBlob, data: []byte("appended base content")}
	baseID := hash.SumSHA1(object.TypeBlob.String(), base.data)

	cases := []struct {
		name     string
		appended map[hash.ObjectID]appendedBase
		fault    func(*faultyAppendFile)
	}{
		{"truncate", nil, func(f *faultyAppendFile) { f.failTruncate = true }},
		{"seek", nil, func(f *faultyAppendFile) { f.failSeek = true }},
		{"writeObject", map[hash.ObjectID]appendedBase{baseID: base}, func(f *faultyAppendFile) { f.failWrite = true }},
		{"writeAt", nil, func(f *faultyAppendFile) { f.failWriteAt = true }},
		{"hash", nil, func(f *faultyAppendFile) { f.failReadAt = true }},
		{"trailer", nil, func(f *faultyAppendFile) { f.failWrite = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := make([]byte, headerSize+hash.Size)
			file := newAppendFaultFile(t, content)
			tc.fault(file)
			entries := make([]Entry, 0)
			offsetByID := make(map[hash.ObjectID]int64)
			if _, _, err := appendMissingBases(file, int64(len(content)), 0, tc.appended, &entries, offsetByID); !errors.Is(err, errAppendFault) {
				t.Fatalf("appendMissingBases returned %v, want wrapping %v", err, errAppendFault)
			}
		})
	}
}

func TestAppendMissingBasesPatchesHeaderAndAppendsInIDOrder(t *testing.T) {
	first := appendedBase{kind: object.TypeBlob, data: []byte("first appended base")}
	second := appendedBase{kind: object.TypeBlob, data: []byte("second appended base, a bit longer")}
	firstID := hash.SumSHA1(object.TypeBlob.String(), first.data)
	secondID := hash.SumSHA1(object.TypeBlob.String(), second.data)
	appended := map[hash.ObjectID]appendedBase{firstID: first, secondID: second}

	content := make([]byte, headerSize+hash.Size)
	file := newAppendFaultFile(t, content)
	entries := make([]Entry, 0)
	offsetByID := make(map[hash.ObjectID]int64)
	checksum, size, err := appendMissingBases(file, int64(len(content)), 3, appended, &entries, offsetByID)
	if err != nil {
		t.Fatalf("appendMissingBases returned error %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if _, ok := offsetByID[firstID]; !ok {
		t.Error("offsetByID does not hold the first appended base")
	}
	if _, ok := offsetByID[secondID]; !ok {
		t.Error("offsetByID does not hold the second appended base")
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	if info.Size() != size {
		t.Fatalf("the file holds %d bytes, appendMissingBases reported %d", info.Size(), size)
	}

	var patched [4]byte
	if _, err := file.ReadAt(patched[:], 8); err != nil {
		t.Fatalf("ReadAt returned error %v", err)
	}
	if got := binary.BigEndian.Uint32(patched[:]); got != 5 {
		t.Fatalf("the header declares %d objects, want 5 (3 original plus 2 appended)", got)
	}

	digest := sha1.New()
	if _, err := io.Copy(digest, io.NewSectionReader(file.File, 0, size-hash.Size)); err != nil {
		t.Fatalf("Copy returned error %v", err)
	}
	var sum [hash.Size]byte
	digest.Sum(sum[:0])
	if hash.ObjectID(sum) != checksum {
		t.Fatalf("the recomputed trailer does not match Checksum")
	}

	var trailer [hash.Size]byte
	if _, err := file.ReadAt(trailer[:], size-hash.Size); err != nil {
		t.Fatalf("ReadAt returned error %v", err)
	}
	if hash.ObjectID(trailer) != checksum {
		t.Fatalf("the file trailer does not match the returned checksum")
	}
}
