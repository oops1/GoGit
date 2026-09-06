package transport

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestEncoderWriteDataRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteData([]byte("hello")); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if got, want := buf.String(), "0009hello"; got != want {
		t.Fatalf("encoded %q, want %q", got, want)
	}
	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if dec.Type() != PktData {
		t.Fatalf("Type() = %v, want PktData", dec.Type())
	}
	if string(dec.Bytes()) != "hello" {
		t.Fatalf("Bytes() = %q, want %q", dec.Bytes(), "hello")
	}
}

func TestEncoderWriteDataEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).WriteData(nil); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if got, want := buf.String(), "0004"; got != want {
		t.Fatalf("encoded %q, want %q", got, want)
	}
	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if dec.Type() != PktData || len(dec.Bytes()) != 0 {
		t.Fatalf("Type() = %v, Bytes() = %q, want empty PktData", dec.Type(), dec.Bytes())
	}
}

func TestEncoderWriteDataTooLong(t *testing.T) {
	var buf bytes.Buffer
	err := NewEncoder(&buf).WriteData(make([]byte, MaxDataLength+1))
	if !errors.Is(err, ErrPktLineTooLong) {
		t.Fatalf("WriteData returned %v, want ErrPktLineTooLong", err)
	}
}

func TestEncoderWriteDataMaxLength(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).WriteData(make([]byte, MaxDataLength)); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if len(dec.Bytes()) != MaxDataLength {
		t.Fatalf("Bytes() has %d bytes, want %d", len(dec.Bytes()), MaxDataLength)
	}
}

func TestEncoderMarkers(t *testing.T) {
	tests := []struct {
		name  string
		write func(*Encoder) error
		want  PktType
		bytes string
	}{
		{"flush", (*Encoder).WriteFlush, PktFlush, "0000"},
		{"delim", (*Encoder).WriteDelim, PktDelim, "0001"},
		{"response-end", (*Encoder).WriteResponseEnd, PktResponseEnd, "0002"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := test.write(NewEncoder(&buf)); err != nil {
				t.Fatalf("write returned error %v", err)
			}
			if buf.String() != test.bytes {
				t.Fatalf("encoded %q, want %q", buf.String(), test.bytes)
			}
			dec := NewDecoder(&buf)
			if !dec.Scan() {
				t.Fatalf("Scan returned false, Err() = %v", dec.Err())
			}
			if dec.Type() != test.want {
				t.Fatalf("Type() = %v, want %v", dec.Type(), test.want)
			}
		})
	}
}

func TestDecoderRejectsNonHexLength(t *testing.T) {
	dec := NewDecoder(strings.NewReader("zzzzhello"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for a non-hex length")
	}
	if !errors.Is(dec.Err(), ErrPktLineBadLength) {
		t.Fatalf("Err() = %v, want ErrPktLineBadLength", dec.Err())
	}
}

func TestDecoderRejectsReservedLength(t *testing.T) {
	dec := NewDecoder(strings.NewReader("0003"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for the reserved length 0003")
	}
	if !errors.Is(dec.Err(), ErrPktLineReserved) {
		t.Fatalf("Err() = %v, want ErrPktLineReserved", dec.Err())
	}
}

func TestDecoderRejectsTooLongLength(t *testing.T) {
	dec := NewDecoder(strings.NewReader("ffff"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for a length above the maximum")
	}
	if !errors.Is(dec.Err(), ErrPktLineTooLong) {
		t.Fatalf("Err() = %v, want ErrPktLineTooLong", dec.Err())
	}
}

func TestDecoderAcceptsMaxLength(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("fff0")
	buf.Write(make([]byte, MaxDataLength))
	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if len(dec.Bytes()) != MaxDataLength {
		t.Fatalf("Bytes() has %d bytes, want %d", len(dec.Bytes()), MaxDataLength)
	}
}

func TestDecoderTruncatedLength(t *testing.T) {
	dec := NewDecoder(strings.NewReader("00"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for a truncated length header")
	}
	if !errors.Is(dec.Err(), ErrPktLineTruncated) {
		t.Fatalf("Err() = %v, want ErrPktLineTruncated", dec.Err())
	}
}

func TestDecoderTruncatedData(t *testing.T) {
	dec := NewDecoder(strings.NewReader("0009hel"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for a truncated payload")
	}
	if !errors.Is(dec.Err(), ErrPktLineTruncated) {
		t.Fatalf("Err() = %v, want ErrPktLineTruncated", dec.Err())
	}
}

func TestDecoderCleanEOFEndsScanWithoutError(t *testing.T) {
	dec := NewDecoder(strings.NewReader(""))
	if dec.Scan() {
		t.Fatalf("Scan returned true on an empty stream")
	}
	if dec.Err() != nil {
		t.Fatalf("Err() = %v, want nil on a clean EOF", dec.Err())
	}
}

func TestDecoderStopsScanningAfterError(t *testing.T) {
	dec := NewDecoder(strings.NewReader("0003"))
	if dec.Scan() {
		t.Fatalf("Scan returned true for a reserved length")
	}
	firstErr := dec.Err()
	if dec.Scan() {
		t.Fatalf("Scan returned true after a prior error")
	}
	if !errors.Is(dec.Err(), firstErr) {
		t.Fatalf("Err() changed across calls: %v then %v", firstErr, dec.Err())
	}
}

func TestDecoderMultiplePackets(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteData([]byte("first")); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteData([]byte("second-longer")); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}

	dec := NewDecoder(&buf)
	var got []string
	for dec.Scan() {
		if dec.Type() == PktFlush {
			break
		}
		got = append(got, string(dec.Bytes()))
	}
	if err := dec.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	want := []string{"first", "second-longer"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("decoded packets %v, want %v", got, want)
	}
}

func TestDecoderBytesAreOverwrittenByNextScan(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteData([]byte("aaaa")); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteData([]byte("bb")); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}

	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	first := dec.Bytes()
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if string(first) == "aaaa" {
		t.Fatalf("Bytes() from the first Scan still reads %q after a second Scan; the reused buffer should have changed it", first)
	}
}

func TestDecoderDoesNotGrowBackingArrayAcrossScans(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	for range 8 {
		if err := enc.WriteData([]byte("payload")); err != nil {
			t.Fatalf("WriteData returned error %v", err)
		}
	}
	dec := NewDecoder(&buf)
	var arrayStart *byte
	for dec.Scan() {
		if len(dec.Bytes()) == 0 {
			continue
		}
		current := &dec.Bytes()[:1][0]
		if arrayStart == nil {
			arrayStart = current
		} else if arrayStart != current {
			t.Fatalf("the decoder allocated a new backing array between scans")
		}
	}
	if err := dec.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestPktTypeString(t *testing.T) {
	tests := []struct {
		typ  PktType
		want string
	}{
		{PktData, "data"},
		{PktFlush, "flush"},
		{PktDelim, "delim"},
		{PktResponseEnd, "response-end"},
		{PktType(99), "unknown"},
	}
	for _, test := range tests {
		if got := test.typ.String(); got != test.want {
			t.Errorf("PktType(%d).String() = %q, want %q", test.typ, got, test.want)
		}
	}
}

func TestEncoderWritePropagatesWriteError(t *testing.T) {
	err := NewEncoder(failingWriter{}).WriteData([]byte("x"))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("WriteData returned %v, want io.ErrClosedPipe", err)
	}
}

func TestEncoderWriteMarkerPropagatesWriteError(t *testing.T) {
	err := NewEncoder(failingWriter{}).WriteFlush()
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("WriteFlush returned %v, want io.ErrClosedPipe", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestEncoderWriteDataPropagatesPayloadWriteError(t *testing.T) {
	w := &failAfterN{n: 1}
	err := NewEncoder(w).WriteData([]byte("payload"))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("WriteData returned %v, want io.ErrClosedPipe", err)
	}
}

type failAfterN struct {
	n     int
	calls int
}

func (f *failAfterN) Write(p []byte) (int, error) {
	f.calls++
	if f.calls > f.n {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
