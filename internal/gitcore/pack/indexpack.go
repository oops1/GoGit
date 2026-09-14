package pack

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

const (
	tempPackPattern  = "incoming-*" + packSuffix
	tempIndexPattern = "incoming-*" + indexSuffix
	keepSuffix       = ".keep"
	keepFileMode     = 0o600
	entryPrealloc    = 4096
)

var openKeepFile = func(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, keepFileMode)
}

type IndexOptions struct {
	Bases    BaseResolver
	FixThin  bool
	Progress progress.Func
	KeepName string
}

type IndexResult struct {
	Checksum  hash.ObjectID
	PackPath  string
	IndexPath string
	KeepPath  string
	Objects   int
	Bytes     int64
}

type appendedBase struct {
	kind object.Type
	data []byte
}

type knownObjects interface {
	Contains(id hash.ObjectID) (bool, error)
}

type receivingCounter struct {
	source   io.Reader
	progress progress.Func
	received int64
}

func (c *receivingCounter) Read(into []byte) (int, error) {
	n, err := c.source.Read(into)
	if n > 0 {
		c.received += int64(n)
		c.progress.Count(progress.PhaseReceiving, c.received, 0)
	}
	return n, err
}

func IndexPack(ctx context.Context, src io.Reader, dir string, opts IndexOptions) (IndexResult, error) {
	temp, tempPath, err := createTempFile(dir, tempPackPattern)
	if err != nil {
		return IndexResult{}, err
	}
	renamed := false
	defer cleanupTempFile(temp, tempPath, &renamed)

	counter := &receivingCounter{source: src, progress: opts.Progress}
	reader, err := NewReader(io.TeeReader(counter, temp))
	if err != nil {
		return IndexResult{}, err
	}
	incoming := newIndexer(ctx, opts, reader.Count())
	if err := incoming.receive(reader); err != nil {
		return IndexResult{}, err
	}
	size := reader.Offset()
	incoming.pack = &Pack{source: temp, size: size, version: packVersion, count: reader.Count(), trailer: reader.Trailer(), settings: newSettings(nil)}
	if err := incoming.resolve(); err != nil {
		return IndexResult{}, err
	}

	checksum := reader.Trailer()
	entries := incoming.entries
	for _, entry := range entries {
		delete(incoming.appended, entry.ID)
	}
	if opts.FixThin && len(incoming.appended) > 0 {
		checksum, size, err = appendMissingBases(temp, size, len(entries), incoming.appended, &entries)
		if err != nil {
			return IndexResult{}, err
		}
	}

	if err := fileClose(temp); err != nil {
		return IndexResult{}, fmt.Errorf("pack: close %s: %w", tempPath, err)
	}

	packPath := filepath.Join(dir, "pack-"+checksum.String()+packSuffix)
	reused, err := placeFile(tempPath, packPath)
	if err != nil {
		return IndexResult{}, err
	}
	renamed = !reused

	indexPath := filepath.Join(dir, "pack-"+checksum.String()+indexSuffix)
	if err := writeIndexFile(dir, indexPath, entries, checksum); err != nil {
		return IndexResult{}, err
	}

	result := IndexResult{
		Checksum:  checksum,
		PackPath:  packPath,
		IndexPath: indexPath,
		Objects:   len(entries),
		Bytes:     size,
	}
	if opts.KeepName != "" {
		keepPath := filepath.Join(dir, "pack-"+checksum.String()+keepSuffix)
		created, err := writeKeepFile(keepPath, opts.KeepName)
		if err != nil {
			return IndexResult{}, err
		}
		if created {
			result.KeepPath = keepPath
		}
	}
	return result, nil
}

func writeKeepFile(path, content string) (bool, error) {
	file, err := openKeepFile(path)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("pack: write %s: %w", path, err)
	}
	_, err = io.WriteString(file, content)
	if err := errors.Join(err, fileClose(file)); err != nil {
		return false, fmt.Errorf("pack: write %s: %w", path, err)
	}
	return true, nil
}

func createTempFile(dir, pattern string) (*os.File, string, error) {
	temp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, "", fmt.Errorf("pack: create temporary file in %s: %w", dir, err)
	}
	return temp, temp.Name(), nil
}

func cleanupTempFile(temp *os.File, tempPath string, renamed *bool) {
	_ = temp.Close()
	if !*renamed {
		_ = os.Remove(tempPath)
	}
}

type indexer struct {
	ctx      context.Context
	bases    BaseResolver
	known    knownObjects
	progress progress.Func
	pack     *Pack
	entries  []Entry
	plain    []int
	byOffset map[int64][]int
	byName   map[hash.ObjectID][]int
	deltas   int64
	resolved int64
	appended map[hash.ObjectID]appendedBase
}

type resolveFrame struct {
	kind     object.Type
	data     []byte
	children []int
}

func newIndexer(ctx context.Context, opts IndexOptions, count int) *indexer {
	known, _ := opts.Bases.(knownObjects)
	return &indexer{
		ctx:      ctx,
		bases:    opts.Bases,
		known:    known,
		progress: opts.Progress,
		entries:  make([]Entry, 0, min(count, entryPrealloc)),
		byOffset: make(map[int64][]int),
		byName:   make(map[hash.ObjectID][]int),
		appended: make(map[hash.ObjectID]appendedBase),
	}
}

func (x *indexer) receive(reader *Reader) error {
	for {
		if err := x.ctx.Err(); err != nil {
			return err
		}
		entry, err := reader.NextObject()
		if errors.Is(err, io.EOF) {
			return reader.expectEnd()
		}
		if err != nil {
			return err
		}
		position := len(x.entries)
		x.entries = append(x.entries, Entry{Offset: entry.Header.Offset, CRC32: entry.CRC32})
		switch entry.Header.Kind {
		case KindOffsetDelta:
			x.byOffset[entry.Header.BaseOffset] = append(x.byOffset[entry.Header.BaseOffset], position)
			x.deltas++
		case KindRefDelta:
			x.byName[entry.Header.BaseID] = append(x.byName[entry.Header.BaseID], position)
			x.deltas++
		default:
			kind := entry.Header.Kind.Type()
			id := hash.SumSHA1(kind.String(), entry.Data)
			if err := x.checkCollision(id, kind, entry.Data); err != nil {
				return err
			}
			x.entries[position].ID = id
			x.plain = append(x.plain, position)
		}
	}
}

func (x *indexer) resolve() error {
	for _, position := range x.plain {
		entry := x.entries[position]
		children := x.childrenOf(entry.Offset, entry.ID)
		if len(children) == 0 {
			continue
		}
		kind, data, err := x.pack.objectAt(entry.Offset, 0)
		if err != nil {
			return err
		}
		if err := x.expand(kind, data, children); err != nil {
			return err
		}
	}
	if err := x.resolveExternal(); err != nil {
		return err
	}
	if x.resolved != x.deltas {
		return fmt.Errorf("%w: %d deltas stay unresolved", ErrBaseNotFound, x.deltas-x.resolved)
	}
	return nil
}

func (x *indexer) resolveExternal() error {
	ids := slices.SortedFunc(maps.Keys(x.byName), func(a, b hash.ObjectID) int { return a.Compare(b) })
	for _, id := range ids {
		children, pending := x.byName[id]
		if !pending {
			continue
		}
		if x.bases == nil {
			return fmt.Errorf("%w: %s", ErrBaseNotFound, id)
		}
		kind, data, err := x.bases.ResolveBase(id, 0)
		if err != nil {
			return err
		}
		delete(x.byName, id)
		x.appended[id] = appendedBase{kind: kind, data: data}
		if err := x.expand(kind, data, children); err != nil {
			return err
		}
	}
	return nil
}

func (x *indexer) childrenOf(offset int64, id hash.ObjectID) []int {
	children := slices.Concat(x.byOffset[offset], x.byName[id])
	delete(x.byOffset, offset)
	delete(x.byName, id)
	return children
}

func (x *indexer) expand(kind object.Type, data []byte, children []int) error {
	stack := []resolveFrame{{kind: kind, data: data, children: children}}
	for len(stack) > 0 {
		top := len(stack) - 1
		frame := stack[top]
		child := frame.children[0]
		if len(frame.children) == 1 {
			stack[top] = resolveFrame{}
			stack = stack[:top]
		} else {
			stack[top].children = frame.children[1:]
		}
		if err := x.ctx.Err(); err != nil {
			return err
		}
		result, err := x.pack.deltaAt(x.entries[child].Offset, frame.data)
		if err != nil {
			return err
		}
		id := hash.SumSHA1(frame.kind.String(), result)
		if err := x.checkCollision(id, frame.kind, result); err != nil {
			return err
		}
		x.entries[child].ID = id
		x.resolved++
		x.progress.Count(progress.PhaseResolving, x.resolved, x.deltas)
		if grand := x.childrenOf(x.entries[child].Offset, id); len(grand) > 0 {
			stack = append(stack, resolveFrame{kind: frame.kind, data: result, children: grand})
		}
	}
	return nil
}

func (x *indexer) checkCollision(id hash.ObjectID, kind object.Type, data []byte) error {
	if x.known == nil {
		return nil
	}
	exists, err := x.known.Contains(id)
	if err != nil || !exists {
		return err
	}
	haveKind, have, err := x.bases.ResolveBase(id, 0)
	if err != nil {
		return err
	}
	if haveKind != kind || !bytes.Equal(have, data) {
		return fmt.Errorf("%w: %s", ErrCollision, id)
	}
	return nil
}

type appendSink interface {
	io.Writer
	io.ReaderAt
	Truncate(size int64) error
	Seek(offset int64, whence int) (int64, error)
	WriteAt(p []byte, off int64) (int, error)
	Name() string
}

func appendMissingBases(temp appendSink, originalSize int64, objectCount int, appended map[hash.ObjectID]appendedBase, entries *[]Entry) (hash.ObjectID, int64, error) {
	ids := slices.SortedFunc(maps.Keys(appended), func(a, b hash.ObjectID) int { return a.Compare(b) })

	truncatedSize := originalSize - hash.Size
	if err := temp.Truncate(truncatedSize); err != nil {
		return hash.Zero, 0, fmt.Errorf("pack: truncate %s: %w", temp.Name(), err)
	}
	if _, err := temp.Seek(truncatedSize, io.SeekStart); err != nil {
		return hash.Zero, 0, fmt.Errorf("pack: seek %s: %w", temp.Name(), err)
	}

	writer := newPackWriter(temp)
	writer.position = truncatedSize
	for _, id := range ids {
		base := appended[id]
		entry, err := writer.writeObject(id, writeChoice{kind: Kind(base.kind), payload: base.data})
		if err != nil {
			return hash.Zero, 0, err
		}
		*entries = append(*entries, entry)
	}

	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(objectCount+len(ids)))
	if _, err := temp.WriteAt(count[:], 8); err != nil {
		return hash.Zero, 0, fmt.Errorf("pack: patch object count in %s: %w", temp.Name(), err)
	}

	digest := sha1.New()
	if _, err := io.Copy(digest, io.NewSectionReader(temp, 0, writer.position)); err != nil {
		return hash.Zero, 0, fmt.Errorf("pack: hash %s: %w", temp.Name(), err)
	}
	var sum [hash.Size]byte
	digest.Sum(sum[:0])
	checksum := hash.ObjectID(sum)
	if _, err := temp.Write(checksum[:]); err != nil {
		return hash.Zero, 0, fmt.Errorf("pack: write trailer to %s: %w", temp.Name(), err)
	}
	return checksum, writer.position + hash.Size, nil
}

func placeFile(tempPath, finalPath string) (bool, error) {
	if alreadyPlaced(finalPath) {
		return true, nil
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return false, fmt.Errorf("pack: rename %s to %s: %w", tempPath, finalPath, err)
	}
	return false, nil
}

func alreadyPlaced(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func writeIndexFile(dir, path string, entries []Entry, checksum hash.ObjectID) error {
	if alreadyPlaced(path) {
		return nil
	}
	temp, tempPath, err := createTempFile(dir, tempIndexPattern)
	if err != nil {
		return err
	}
	renamed := false
	defer cleanupTempFile(temp, tempPath, &renamed)

	if err := WriteIndex(temp, entries, checksum); err != nil {
		return err
	}
	if err := fileClose(temp); err != nil {
		return fmt.Errorf("pack: close %s: %w", tempPath, err)
	}
	reused, err := placeFile(tempPath, path)
	if err != nil {
		return err
	}
	renamed = !reused
	return nil
}
