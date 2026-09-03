package pack

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestApplyDeltaCopiesAndInserts(t *testing.T) {
	base := []byte("the quick brown fox jumps over the lazy dog")
	delta := slices.Concat(
		deltaSizes(int64(len(base)), 22),
		copyOp(4, 5),
		insertOp([]byte("BLACK")),
		copyOp(15, 12),
	)
	got, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if want := "quickBLACK fox jumps o"; string(got) != want {
		t.Fatalf("ApplyDelta produced %q, want %q", got, want)
	}
}

func TestApplyDeltaTreatsZeroSizeAsSixtyFourKilobytes(t *testing.T) {
	base := bytes.Repeat([]byte("a"), defaultCopySize+16)
	delta := slices.Concat(deltaSizes(int64(len(base)), defaultCopySize), copyOp(0, defaultCopySize))
	got, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if len(got) != defaultCopySize {
		t.Fatalf("ApplyDelta produced %d bytes, want %d", len(got), defaultCopySize)
	}
}

func TestApplyDeltaReadsWideOffsetsAndSizes(t *testing.T) {
	base := bytes.Repeat([]byte("x"), 0x20000+300)
	delta := slices.Concat(deltaSizes(int64(len(base)), 0x10001), copyOp(0x10001, 0x10001))
	got, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if len(got) != 0x10001 {
		t.Fatalf("ApplyDelta produced %d bytes, want %d", len(got), 0x10001)
	}
}

func TestApplyDeltaRejectsBrokenStreams(t *testing.T) {
	base := []byte("0123456789")
	cases := []struct {
		name  string
		delta []byte
		want  error
	}{
		{"emptyStream", nil, ErrInvalidDelta},
		{"truncatedSourceSize", []byte{continuation}, ErrInvalidDelta},
		{"truncatedTargetSize", []byte{10}, ErrInvalidDelta},
		{"sourceSizeOverflow", append(bytes.Repeat([]byte{continuation | 1}, 10), 1), ErrInvalidDelta},
		{"negativeSourceSize", append(bytes.Repeat([]byte{continuation | 0x7f}, 9), 0x7f), ErrInvalidDelta},
		{"baseSizeMismatch", slices.Concat(deltaSizes(9, 1), insertOp([]byte("a"))), ErrInvalidDelta},
		{"reservedOpcode", slices.Concat(deltaSizes(10, 1), []byte{0}), ErrInvalidDelta},
		{"truncatedCopyOffset", slices.Concat(deltaSizes(10, 1), []byte{copyOpcode | 0x01}), ErrInvalidDelta},
		{"truncatedCopySize", slices.Concat(deltaSizes(10, 1), []byte{copyOpcode | 0x10}), ErrInvalidDelta},
		{"copyPastBase", slices.Concat(deltaSizes(10, 4), copyOp(8, 4)), ErrInvalidDelta},
		{"copyPastTarget", slices.Concat(deltaSizes(10, 2), copyOp(0, 4)), ErrInvalidDelta},
		{"truncatedInsert", slices.Concat(deltaSizes(10, 4), []byte{4, 'a'}), ErrInvalidDelta},
		{"insertPastTarget", slices.Concat(deltaSizes(10, 2), insertOp([]byte("abcd"))), ErrInvalidDelta},
		{"shortResult", slices.Concat(deltaSizes(10, 4), insertOp([]byte("ab"))), ErrDeltaSizeMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyDelta(base, tc.delta)
			if got != nil {
				t.Fatalf("ApplyDelta produced %q, want no data", got)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("ApplyDelta returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestApplyDeltaRejectsTargetSizeOverflow(t *testing.T) {
	base := []byte("0123456789")
	delta := slices.Concat(encodeDeltaSize(10), bytes.Repeat([]byte{continuation | 1}, 10), []byte{1})
	if _, err := ApplyDelta(base, delta); !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("ApplyDelta returned %v, want %v", err, ErrInvalidDelta)
	}
	if err := errorText(t, base, delta); !strings.Contains(err, "target") {
		t.Fatalf("the error %q does not name the target size", err)
	}
}

func errorText(t *testing.T, base, delta []byte) string {
	t.Helper()
	_, err := ApplyDelta(base, delta)
	if err == nil {
		t.Fatal("ApplyDelta returned no error")
	}
	return err.Error()
}

func TestEncodeDeltaSizeRoundTrips(t *testing.T) {
	for _, size := range []int64{0, 1, 0x7f, 0x80, 0x3fff, 1 << 20, 1<<62 - 1} {
		encoded := encodeDeltaSize(size)
		got, read, err := decodeDeltaSize(encoded, "source")
		if err != nil {
			t.Fatalf("decodeDeltaSize(%d) returned error %v", size, err)
		}
		if got != size || read != len(encoded) {
			t.Fatalf("decodeDeltaSize(%d) = (%d, %d), want (%d, %d)", size, got, read, size, len(encoded))
		}
	}
}

func TestPayloadPoolReusesBuffers(t *testing.T) {
	first := acquirePayload(64)
	if len(first.data) != 64 {
		t.Fatalf("acquirePayload gave %d bytes, want 64", len(first.data))
	}
	releasePayload(first)
	second := acquirePayload(16)
	if len(second.data) != 16 {
		t.Fatalf("acquirePayload gave %d bytes, want 16", len(second.data))
	}
	releasePayload(second)
}
