package transport

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestParseAckLineVariants(t *testing.T) {
	tests := []struct {
		line       string
		wantStatus ackStatus
		wantErr    bool
	}{
		{"NAK", ackNAK, false},
		{"ACK " + idOf(1).String(), ackBare, false},
		{"ACK " + idOf(1).String() + " continue", ackContinue, false},
		{"ACK " + idOf(1).String() + " common", ackCommon, false},
		{"ACK " + idOf(1).String() + " ready", ackReady, false},
		{"ACK " + idOf(1).String() + " bogus", 0, true},
		{"garbage", 0, true},
		{"ACK not-an-oid", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			ack, err := parseAckLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseAckLine(%q) succeeded, want an error", tt.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAckLine(%q) returned error %v", tt.line, err)
			}
			if ack.status != tt.wantStatus {
				t.Fatalf("parseAckLine(%q).status = %v, want %v", tt.line, ack.status, tt.wantStatus)
			}
		})
	}
}

func TestApplyAckOnNilNegotiatorIsNoOp(t *testing.T) {
	applyAck(nil, ackLine{id: idOf(1), status: ackBare})
}

func TestApplyAckSkipsZeroID(t *testing.T) {
	neg := &sliceNegotiator{}
	applyAck(neg, ackLine{status: ackReady})
	if len(neg.common) != 0 {
		t.Fatalf("Common was called for a zero id ack")
	}
}

func TestReadShallowUpdate(t *testing.T) {
	body := newPktBuilder().
		line("shallow " + idOf(1).String() + "\n").
		line("unshallow " + idOf(2).String() + "\n").
		flush().bytes()
	shallow, unshallow, err := readShallowUpdate(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("readShallowUpdate returned error %v", err)
	}
	if len(shallow) != 1 || shallow[0] != idOf(1) {
		t.Fatalf("shallow = %v", shallow)
	}
	if len(unshallow) != 1 || unshallow[0] != idOf(2) {
		t.Fatalf("unshallow = %v", unshallow)
	}
}

func TestReadShallowUpdateRejectsUnexpectedLine(t *testing.T) {
	body := newPktBuilder().line("bogus\n").flush().bytes()
	_, _, err := readShallowUpdate(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("readShallowUpdate returned %v, want ErrProtocol", err)
	}
}

func TestReadShallowUpdateRejectsMalformedOid(t *testing.T) {
	body := newPktBuilder().line("shallow not-an-oid\n").flush().bytes()
	_, _, err := readShallowUpdate(NewDecoder(bytes.NewReader(body)))
	if err == nil {
		t.Fatalf("readShallowUpdate succeeded on a malformed oid")
	}
	body2 := newPktBuilder().line("unshallow not-an-oid\n").flush().bytes()
	_, _, err = readShallowUpdate(NewDecoder(bytes.NewReader(body2)))
	if err == nil {
		t.Fatalf("readShallowUpdate succeeded on a malformed unshallow oid")
	}
}

func TestReadShallowUpdatePropagatesReadError(t *testing.T) {
	_, _, err := readShallowUpdate(NewDecoder(bytes.NewReader([]byte("000"))))
	if err == nil {
		t.Fatalf("readShallowUpdate succeeded on a truncated stream")
	}
}

func TestReadShallowUpdateSkipsNonDataPackets(t *testing.T) {
	body := newPktBuilder().delim().line("shallow " + idOf(1).String() + "\n").flush().bytes()
	shallow, _, err := readShallowUpdate(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("readShallowUpdate returned error %v", err)
	}
	if len(shallow) != 1 || shallow[0] != idOf(1) {
		t.Fatalf("shallow = %v", shallow)
	}
}

func TestReadOnePktLineOnEmptyStream(t *testing.T) {
	_, _, err := readOnePktLine(NewDecoder(bytes.NewReader(nil)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("readOnePktLine returned %v, want ErrProtocol", err)
	}
}

func TestAgentValueDefaultsWhenEmpty(t *testing.T) {
	if got := agentValue(Options{}); got != defaultAgent {
		t.Fatalf("agentValue(empty) = %q, want %q", got, defaultAgent)
	}
	if got := agentValue(Options{UserAgent: "custom/1.0"}); got != "custom/1.0" {
		t.Fatalf("agentValue = %q, want custom/1.0", got)
	}
}

func TestAppendDeepenArgsAllVariants(t *testing.T) {
	req := FetchRequest{
		Depth:       5,
		DeepenSince: time.Unix(1000, 0).UTC(),
		DeepenNot:   []string{"refs/heads/old"},
	}
	body := appendDeepenArgs(nil, req, appendPktLine)
	text := string(body)
	for _, want := range []string{"deepen 5\n", "deepen-since 1000\n", "deepen-not refs/heads/old\n"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Fatalf("body %q missing %q", text, want)
		}
	}
}

func TestPullHavesPropagatesIteratorError(t *testing.T) {
	boom := errors.New("boom")
	next := haveIterFunc(func(yield func(hash.ObjectID, error) bool) {
		if !yield(idOf(1), nil) {
			return
		}
		yield(hash.Zero, boom)
	})
	batch, exhausted, err := pullHaves(next, maxHavesPerRound)
	if !errors.Is(err, boom) {
		t.Fatalf("pullHaves returned error %v, want boom", err)
	}
	if exhausted {
		t.Fatalf("exhausted = true, want false")
	}
	if len(batch) != 1 || batch[0] != idOf(1) {
		t.Fatalf("batch = %v, want [idOf(1)]", batch)
	}
}

func TestHaveIterFuncStopsAfterExhaustion(t *testing.T) {
	next := haveIterFunc(func(yield func(hash.ObjectID, error) bool) {
		yield(idOf(1), nil)
	})
	if id, err, ok := next(); !ok || err != nil || id != idOf(1) {
		t.Fatalf("first next() = (%v, %v, %v)", id, err, ok)
	}
	if _, _, ok := next(); ok {
		t.Fatalf("second next() reported ok=true, want exhausted")
	}
	if _, _, ok := next(); ok {
		t.Fatalf("third next() reported ok=true, want it to stay exhausted")
	}
}

func TestCloseQuietlyIgnoresError(t *testing.T) {
	closeQuietly(failingCloser{})
}

type failingCloser struct{}

func (failingCloser) Close() error { return errors.New("boom") }
