package pack

import (
	"bytes"
	"testing"
)

func TestEncodeDeltaRoundTripsThroughApplyDelta(t *testing.T) {
	base := similarBlob(0, 400)
	target := similarBlob(3, 400)
	delta := EncodeDelta(base, target)
	if len(delta) >= len(target) {
		t.Fatalf("delta holds %d bytes, target holds %d, want a smaller delta", len(delta), len(target))
	}
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if !bytes.Equal(rebuilt, target) {
		t.Fatal("ApplyDelta(base, EncodeDelta(base, target)) did not reproduce target")
	}
}

func TestEncodeDeltaHandlesABaseShorterThanTheHashWindow(t *testing.T) {
	base := []byte("ab")
	target := []byte("abcdefgh")
	delta := EncodeDelta(base, target)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if !bytes.Equal(rebuilt, target) {
		t.Fatal("ApplyDelta did not reproduce target for a too-short base")
	}
}

func TestEncodeDeltaHandlesEmptyBaseAndTarget(t *testing.T) {
	for _, tc := range [][2][]byte{
		{nil, nil},
		{nil, []byte("target only")},
		{[]byte("base only"), nil},
	} {
		delta := EncodeDelta(tc[0], tc[1])
		rebuilt, err := ApplyDelta(tc[0], delta)
		if err != nil {
			t.Fatalf("ApplyDelta returned error %v", err)
		}
		if !bytes.Equal(rebuilt, tc[1]) {
			t.Fatalf("ApplyDelta gave %q, want %q", rebuilt, tc[1])
		}
	}
}

func TestEncodeDeltaSplitsInsertsLongerThanTheOpcodeLimit(t *testing.T) {
	base := []byte("unrelated base content that shares nothing")
	target := bytes.Repeat([]byte("z"), deltaMaxInsert*3+5)
	delta := EncodeDelta(base, target)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if !bytes.Equal(rebuilt, target) {
		t.Fatal("ApplyDelta did not reproduce a target requiring multiple insert opcodes")
	}
}

func TestAppendDeltaCopiesSplitsAtTheChunkLimit(t *testing.T) {
	size := deltaMaxCopy + 100
	base := make([]byte, size)
	for i := range base {
		base[i] = byte(i)
	}
	copyOps := appendDeltaCopies(nil, 0, size)
	delta := appendDeltaSize(nil, int64(size))
	delta = appendDeltaSize(delta, int64(size))
	delta = append(delta, copyOps...)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if !bytes.Equal(rebuilt, base) {
		t.Fatal("ApplyDelta did not reproduce a copy split across multiple opcodes")
	}
}

func TestAppendDeltaCopyOmitsSizeBytesForTheDefaultChunkSize(t *testing.T) {
	const chunk = 65536
	base := bytes.Repeat([]byte("A"), chunk+0x1234+16)
	copyOp := appendDeltaCopy(nil, 0x1234, chunk)
	if len(copyOp) != 3 {
		t.Fatalf("encoded copy holds %d bytes, want 3 (opcode + 2 offset bytes)", len(copyOp))
	}
	delta := appendDeltaSize(nil, int64(len(base)))
	delta = appendDeltaSize(delta, chunk)
	delta = append(delta, copyOp...)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if want := base[0x1234 : 0x1234+chunk]; !bytes.Equal(rebuilt, want) {
		t.Fatal("ApplyDelta did not reproduce the expected default-size copy")
	}
}

func TestAppendDeltaCopyKeepsSizeBytesJustAboveTheDefaultChunkSize(t *testing.T) {
	const chunk = 65537
	base := bytes.Repeat([]byte("A"), chunk+0x1234+16)
	copyOp := appendDeltaCopy(nil, 0x1234, chunk)
	if len(copyOp) == 3 {
		t.Fatalf("encoded copy holds %d bytes, the default-chunk-size shortcut fired for a size one byte above it", len(copyOp))
	}
	if len(copyOp) != 5 {
		t.Fatalf("encoded copy holds %d bytes, want 5 (opcode + 2 offset bytes + 2 non-contiguous size bytes)", len(copyOp))
	}
	delta := appendDeltaSize(nil, int64(len(base)))
	delta = appendDeltaSize(delta, chunk)
	delta = append(delta, copyOp...)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if want := base[0x1234 : 0x1234+chunk]; !bytes.Equal(rebuilt, want) {
		t.Fatal("ApplyDelta did not reproduce the expected just-above-default-size copy")
	}
}

func TestAppendDeltaCopyOmitsZeroOffsetBytes(t *testing.T) {
	base := bytes.Repeat([]byte("B"), 32)
	copyOp := appendDeltaCopy(nil, 0, 10)
	if copyOp[0]&0x0f != 0 {
		t.Fatalf("opcode %08b encodes nonzero offset flags for a zero offset", copyOp[0])
	}
	delta := appendDeltaSize(nil, int64(len(base)))
	delta = appendDeltaSize(delta, 10)
	delta = append(delta, copyOp...)
	rebuilt, err := ApplyDelta(base, delta)
	if err != nil {
		t.Fatalf("ApplyDelta returned error %v", err)
	}
	if !bytes.Equal(rebuilt, base[:10]) {
		t.Fatal("ApplyDelta did not reproduce the expected zero-offset copy")
	}
}
