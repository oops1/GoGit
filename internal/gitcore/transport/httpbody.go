package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const gzipRequestThreshold = 1024

var (
	createPushSpool      = func() (*os.File, error) { return os.CreateTemp("", "gogit-push-*.pack") }
	errBodyNotReplayable = errors.New("transport: the request body cannot be sent twice")
)

type requestBody struct {
	open   func() (io.ReadCloser, error)
	length int64
}

func bytesBody(data []byte) *requestBody {
	return &requestBody{
		length: int64(len(data)),
		open:   func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	}
}

func streamBody(r io.Reader) *requestBody {
	var used atomic.Bool
	return &requestBody{
		length: -1,
		open: func() (io.ReadCloser, error) {
			if used.Swap(true) {
				return nil, errBodyNotReplayable
			}
			return io.NopCloser(r), nil
		},
	}
}

func pushBody(prefix []byte, pack io.Reader, postBuffer int64, replayable bool) (*requestBody, func(), error) {
	head, err := io.ReadAll(io.LimitReader(pack, postBuffer+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reading the push pack: %w", ErrProtocol, err)
	}
	if int64(len(head)) <= postBuffer {
		return bytesBody(slices.Concat(prefix, head)), func() {}, nil
	}
	rest := io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(head), pack)
	if !replayable {
		return streamBody(rest), func() {}, nil
	}
	return spoolBody(rest)
}

func spoolBody(content io.Reader) (*requestBody, func(), error) {
	file, err := createPushSpool()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: spooling the push pack: %w", ErrProtocol, err)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	size, err := io.Copy(file, content)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("%w: reading the push pack: %w", ErrProtocol, err)
	}
	body := &requestBody{
		length: -1,
		open: func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(file, 0, size)), nil
		},
	}
	return body, cleanup, nil
}

func attachBody(req *http.Request, body *requestBody, watch *lowSpeedWatch) error {
	rc, err := body.open()
	if err != nil {
		return err
	}
	req.Body = countingBody{ReadCloser: rc, watch: watch}
	req.ContentLength = body.length
	req.GetBody = func() (io.ReadCloser, error) {
		rc, err := body.open()
		if err != nil {
			return nil, err
		}
		return countingBody{ReadCloser: rc, watch: watch}, nil
	}
	return nil
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

type lowSpeedWatch struct {
	limit    int64
	window   time.Duration
	bytes    atomic.Int64
	done     chan struct{}
	stopOnce sync.Once
}

func startLowSpeedWatch(limit, seconds int64, abort context.CancelCauseFunc) *lowSpeedWatch {
	if limit <= 0 || seconds <= 0 {
		return nil
	}
	w := &lowSpeedWatch{limit: limit * seconds, window: time.Duration(seconds) * time.Second, done: make(chan struct{})}
	go w.run(abort)
	return w
}

func (w *lowSpeedWatch) run(abort context.CancelCauseFunc) {
	ticker := time.NewTicker(w.window)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if w.bytes.Swap(0) < w.limit {
				abort(ErrLowSpeed)
				return
			}
		}
	}
}

func (w *lowSpeedWatch) add(n int) {
	if w != nil {
		w.bytes.Add(int64(n))
	}
}

func (w *lowSpeedWatch) stop() {
	if w != nil {
		w.stopOnce.Do(func() { close(w.done) })
	}
}

type countingBody struct {
	io.ReadCloser
	watch *lowSpeedWatch
}

func (b countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.watch.add(n)
	return n, err
}

type responseBody struct {
	io.ReadCloser
	watch  *lowSpeedWatch
	ctx    context.Context
	cancel context.CancelCauseFunc
}

func (b *responseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.watch.add(n)
	if err != nil {
		err = transferError(err, context.Cause(b.ctx))
	}
	return n, err
}

func (b *responseBody) Close() error {
	b.watch.stop()
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}

func transferError(err, cause error) error {
	if errors.Is(cause, ErrLowSpeed) || errors.Is(cause, ErrHeaderTimeout) {
		return cause
	}
	return err
}
