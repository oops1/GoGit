package pack

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	entryPrealloc    = 4096
)

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
	Objects   int
	Bytes     int64
}

type appendedBase struct {
	kind object.Type
	data []byte
}

type indexResolver struct {
	pack       *Pack
	offsetByID map[hash.ObjectID]int64
	bases      BaseResolver
	appended   map[hash.ObjectID]appendedBase
}

func (r *indexResolver) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	if offset, ok := r.offsetByID[id]; ok {
		return r.pack.objectAt(offset, depth)
	}
	if r.bases == nil {
		return 0, nil, fmt.Errorf("%w: %s", ErrBaseNotFound, id)
	}
	kind, data, err := r.bases.ResolveBase(id, depth)
	if err != nil {
		return 0, nil, err
	}
	if _, ok := r.appended[id]; !ok {
		r.appended[id] = appendedBase{kind: kind, data: data}
	}
	return kind, data, nil
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

	entries, offsetByID, deltaPositions, err := readIncomingObjects(ctx, reader)
	if err != nil {
		return IndexResult{}, err
	}
	originalSize := counter.received

	resolver := &indexResolver{offsetByID: offsetByID, bases: opts.Bases, appended: make(map[hash.ObjectID]appendedBase)}
	tempPack, err := NewPack(temp, originalSize, WithBaseResolver(resolver))
	if err != nil {
		return IndexResult{}, err
	}
	resolver.pack = tempPack

	if err := resolveDeltas(ctx, tempPack, entries, deltaPositions, offsetByID, opts.Progress); err != nil {
		return IndexResult{}, err
	}

	checksum := reader.Trailer()
	size := originalSize
	if opts.FixThin && len(resolver.appended) > 0 {
		checksum, size, err = appendMissingBases(temp, originalSize, len(entries), resolver.appended, &entries, offsetByID)
		if err != nil {
			return IndexResult{}, err
		}
	}

	if err := temp.Close(); err != nil {
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

	if opts.KeepName != "" {
		keepPath := filepath.Join(dir, "pack-"+checksum.String()+keepSuffix)
		if err := os.WriteFile(keepPath, []byte(opts.KeepName), 0o600); err != nil {
			return IndexResult{}, fmt.Errorf("pack: write %s: %w", keepPath, err)
		}
	}

	return IndexResult{
		Checksum:  checksum,
		PackPath:  packPath,
		IndexPath: indexPath,
		Objects:   len(entries),
		Bytes:     size,
	}, nil
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

func readIncomingObjects(ctx context.Context, reader *Reader) ([]Entry, map[hash.ObjectID]int64, []int, error) {
	entries := make([]Entry, 0, min(reader.Count(), entryPrealloc))
	offsetByID := make(map[hash.ObjectID]int64, min(reader.Count(), entryPrealloc))
	var deltaPositions []int
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		entry, err := reader.NextObject()
		if errors.Is(err, io.EOF) {
			return entries, offsetByID, deltaPositions, nil
		}
		if err != nil {
			return nil, nil, nil, err
		}
		if entry.Header.Kind.IsDelta() {
			entries = append(entries, Entry{Offset: entry.Header.Offset, CRC32: entry.CRC32})
			deltaPositions = append(deltaPositions, len(entries)-1)
			continue
		}
		id := hash.SumSHA1(entry.Header.Kind.Type().String(), entry.Data)
		entries = append(entries, Entry{ID: id, Offset: entry.Header.Offset, CRC32: entry.CRC32})
		offsetByID[id] = entry.Header.Offset
	}
}

func resolveDeltas(ctx context.Context, tempPack *Pack, entries []Entry, positions []int, offsetByID map[hash.ObjectID]int64, prog progress.Func) error {
	total := int64(len(positions))
	resolved := int64(0)
	remaining := positions
	for len(remaining) > 0 {
		pending := make([]int, 0, len(remaining))
		var lastErr error
		progressed := false
		for _, position := range remaining {
			if err := ctx.Err(); err != nil {
				return err
			}
			kind, data, err := tempPack.objectAt(entries[position].Offset, 0)
			if err != nil {
				pending = append(pending, position)
				lastErr = err
				continue
			}
			id := hash.SumSHA1(kind.String(), data)
			entries[position].ID = id
			offsetByID[id] = entries[position].Offset
			resolved++
			prog.Count(progress.PhaseResolving, resolved, total)
			progressed = true
		}
		if !progressed {
			return lastErr
		}
		remaining = pending
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

func appendMissingBases(temp appendSink, originalSize int64, objectCount int, appended map[hash.ObjectID]appendedBase, entries *[]Entry, offsetByID map[hash.ObjectID]int64) (hash.ObjectID, int64, error) {
	ids := make([]hash.ObjectID, 0, len(appended))
	for id := range appended {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b hash.ObjectID) int { return a.Compare(b) })

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
		offsetByID[id] = entry.Offset
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
	if err := temp.Close(); err != nil {
		return fmt.Errorf("pack: close %s: %w", tempPath, err)
	}
	reused, err := placeFile(tempPath, path)
	if err != nil {
		return err
	}
	renamed = !reused
	return nil
}
