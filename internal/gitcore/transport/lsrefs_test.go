package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestParseLsRefsLineMalformed(t *testing.T) {
	if _, _, err := parseLsRefsLine("no-space-here"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseLsRefsLine returned %v, want ErrProtocol", err)
	}
	if _, _, err := parseLsRefsLine("not-an-oid refs/heads/main"); err == nil {
		t.Fatalf("parseLsRefsLine succeeded on a malformed oid")
	}
	if _, _, err := parseLsRefsLine(idOf(1).String() + " refs/tags/v1 peeled:not-an-oid"); err == nil {
		t.Fatalf("parseLsRefsLine succeeded on a malformed peeled oid")
	}
}

func TestParseLsRefsResponseSkipsNonDataPackets(t *testing.T) {
	body := newPktBuilder().
		delim().
		line(idOf(1).String() + " refs/heads/main\n").
		flush().bytes()
	refs, _, err := parseLsRefsResponse(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseLsRefsResponse returned error %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %v, want 1 entry", refs)
	}
}

func TestParseLsRefsResponseRejectsMalformedLine(t *testing.T) {
	body := newPktBuilder().line("bogus\n").flush().bytes()
	_, _, err := parseLsRefsResponse(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseLsRefsResponse returned %v, want ErrProtocol", err)
	}
}

func TestParseLsRefsResponsePropagatesReadError(t *testing.T) {
	_, _, err := parseLsRefsResponse(NewDecoder(bytes.NewReader([]byte("000"))))
	if err == nil {
		t.Fatalf("parseLsRefsResponse succeeded on a truncated stream")
	}
}

type errorRoundTripper struct{ err error }

func (rt errorRoundTripper) round(context.Context, []byte) (io.ReadCloser, error) {
	return nil, rt.err
}

func TestLsRefsV2PropagatesRoundTripperError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := lsRefsV2(t.Context(), errorRoundTripper{err: boom}, "gogit")
	if !errors.Is(err, boom) {
		t.Fatalf("lsRefsV2 returned %v, want boom", err)
	}
}
