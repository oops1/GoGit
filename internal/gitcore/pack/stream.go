package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const maxDeltaSizeBytes = 10

func (p *Pack) InfoAt(offset int64) (object.Type, int64, error) {
	head, err := p.HeaderAt(offset)
	if err != nil {
		return 0, 0, err
	}
	if !head.Kind.IsDelta() {
		return head.Kind.Type(), head.Size, nil
	}
	size, err := p.deltaResultSize(head)
	if err != nil {
		return 0, 0, err
	}
	kind, err := p.typeAt(head, 1)
	if err != nil {
		return 0, 0, err
	}
	return kind, size, nil
}

func (p *Pack) deltaResultSize(head ObjectHeader) (int64, error) {
	reader, err := acquireInflater(p.dataReader(head))
	if err != nil {
		return 0, fmt.Errorf("pack: delta at %d: %w", head.Offset, err)
	}
	defer releaseInflater(reader)
	var raw [2 * maxDeltaSizeBytes]byte
	prefix := raw[:min(int64(len(raw)), head.Size)]
	if _, err := io.ReadFull(reader, prefix); err != nil {
		return 0, fmt.Errorf("%w: delta at %d: %v", ErrDecompress, head.Offset, err)
	}
	_, used, err := decodeDeltaSize(prefix, "source")
	if err != nil {
		return 0, fmt.Errorf("pack: delta at %d: %w", head.Offset, err)
	}
	size, _, err := decodeDeltaSize(prefix[used:], "target")
	if err != nil {
		return 0, fmt.Errorf("pack: delta at %d: %w", head.Offset, err)
	}
	return size, nil
}

func (p *Pack) typeAt(head ObjectHeader, depth int) (object.Type, error) {
	for head.Kind.IsDelta() {
		if depth > p.settings.maxDepth {
			return 0, fmt.Errorf("%w: %d links reached at %d", ErrDeltaChainTooDeep, depth, head.Offset)
		}
		offset := head.BaseOffset
		if head.Kind == KindRefDelta {
			found, ok, err := p.localBase(head.BaseID)
			if err != nil {
				return 0, err
			}
			if !ok {
				kind, _, err := p.baseOf(head, depth)
				return kind, err
			}
			offset = found
		}
		next, err := p.HeaderAt(offset)
		if err != nil {
			return 0, err
		}
		head = next
		depth++
	}
	return head.Kind.Type(), nil
}

func (p *Pack) localBase(id hash.ObjectID) (int64, bool, error) {
	if p.settings.index == nil {
		return 0, false, nil
	}
	return p.settings.index.Lookup(id)
}

func (p *Pack) StreamAt(offset int64) (object.Type, int64, io.ReadCloser, error) {
	head, err := p.HeaderAt(offset)
	if err != nil {
		return 0, 0, nil, err
	}
	if head.Kind.IsDelta() {
		kind, data, err := p.objectAt(offset, 0)
		if err != nil {
			return 0, 0, nil, err
		}
		return kind, int64(len(data)), io.NopCloser(bytes.NewReader(data)), nil
	}
	if err := p.checkSize(head); err != nil {
		return 0, 0, nil, err
	}
	reader, err := acquireInflater(p.dataReader(head))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("pack: object at %d: %w", offset, err)
	}
	return head.Kind.Type(), head.Size, &inflateStream{source: reader, offset: offset, size: head.Size, remaining: head.Size}, nil
}

type inflateStream struct {
	source    inflater
	offset    int64
	size      int64
	remaining int64
	finished  bool
	err       error
}

func (s *inflateStream) Read(into []byte) (int, error) {
	if s.source == nil {
		return 0, os.ErrClosed
	}
	if s.remaining == 0 {
		return 0, s.finish()
	}
	if int64(len(into)) > s.remaining {
		into = into[:s.remaining]
	}
	read, err := s.source.Read(into)
	s.remaining -= int64(read)
	if err == nil || errors.Is(err, io.EOF) && s.remaining == 0 {
		return read, nil
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return read, fmt.Errorf("%w: object at %d: %v", ErrDecompress, s.offset, err)
}

func (s *inflateStream) finish() error {
	if s.finished {
		return s.err
	}
	s.finished = true
	s.err = io.EOF
	if err := checkStreamEnd(s.source, s.size); err != nil {
		s.err = fmt.Errorf("pack: object at %d: %w", s.offset, err)
	}
	return s.err
}

func (s *inflateStream) Close() error {
	if s.source != nil {
		releaseInflater(s.source)
		s.source = nil
	}
	return nil
}

func (s *Store) Stream(id hash.ObjectID) (object.Type, int64, io.ReadCloser, bool, error) {
	files := s.acquire()
	defer release(files)
	for _, file := range files {
		offset, ok, err := file.Index.Lookup(id)
		if err != nil {
			return 0, 0, nil, false, err
		}
		if !ok {
			continue
		}
		kind, size, reader, err := file.Pack.StreamAt(offset)
		if err != nil {
			return 0, 0, nil, false, err
		}
		file.acquire()
		return kind, size, &heldStream{ReadCloser: reader, file: file}, true, nil
	}
	return 0, 0, nil, false, nil
}

type heldStream struct {
	io.ReadCloser
	file    *PackFile
	release sync.Once
}

func (h *heldStream) Close() error {
	err := h.ReadCloser.Close()
	h.release.Do(h.file.release)
	return err
}

func (s *Store) Forget(names ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]*PackFile, 0, len(s.files))
	var failures []error
	for _, file := range s.files {
		if slices.Contains(names, file.Name) {
			failures = append(failures, s.retire(file))
			continue
		}
		kept = append(kept, file)
	}
	s.files = kept
	return errors.Join(failures...)
}
