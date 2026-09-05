package transport

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/progress"
)

func TestSidebandReaderDeliversOnlyPackChannel(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, 0)
	var progressLines []string
	report := progress.Func(func(r progress.Report) {
		progressLines = append(progressLines, r.Message)
	})
	if err := mux.WriteProgress([]byte("counting objects\n")); err != nil {
		t.Fatalf("WriteProgress returned error %v", err)
	}
	if err := mux.WritePack([]byte("PACKDATA")); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if err := mux.WriteProgress([]byte("done\n")); err != nil {
		t.Fatalf("WriteProgress returned error %v", err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatalf("Flush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, report)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	if string(got) != "PACKDATA" {
		t.Fatalf("ReadAll returned %q, want %q", got, "PACKDATA")
	}
	if len(progressLines) != 2 || progressLines[0] != "counting objects\n" || progressLines[1] != "done\n" {
		t.Fatalf("progress lines = %v, want two counting/done lines", progressLines)
	}
}

func TestSidebandReaderSmallReadBuffer(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, 0)
	if err := mux.WritePack([]byte("0123456789")); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatalf("Flush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	small := make([]byte, 3)
	var got []byte
	for {
		n, err := reader.Read(small)
		got = append(got, small[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Read returned error %v", err)
		}
	}
	if string(got) != "0123456789" {
		t.Fatalf("assembled %q, want %q", got, "0123456789")
	}
}

func TestSidebandReaderTranslatesErrorChannel(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, 0)
	if err := mux.WriteError([]byte("remote rejected the request\n")); err != nil {
		t.Fatalf("WriteError returned error %v", err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatalf("Flush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	_, err := reader.Read(make([]byte, 64))
	if !errors.Is(err, ErrSidebandRemote) {
		t.Fatalf("Read returned %v, want ErrSidebandRemote", err)
	}
	if !strings.Contains(err.Error(), "remote rejected the request") {
		t.Fatalf("Read error %v does not mention the remote message", err)
	}
}

func TestSidebandReaderStopsAtResponseEnd(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteData([]byte{SidebandPack, 'a', 'b'}); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteResponseEnd(); err != nil {
		t.Fatalf("WriteResponseEnd returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("ReadAll returned %q, want %q", got, "ab")
	}
}

func TestSidebandReaderSkipsDelimAndEmptyPackets(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteDelim(); err != nil {
		t.Fatalf("WriteDelim returned error %v", err)
	}
	if err := enc.WriteData(nil); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteData([]byte{SidebandPack, 'z'}); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	if string(got) != "z" {
		t.Fatalf("ReadAll returned %q, want %q", got, "z")
	}
}

func TestSidebandReaderRejectsUnknownChannel(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteData([]byte{9, 'x'}); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	_, err := reader.Read(make([]byte, 16))
	if !errors.Is(err, ErrSidebandChannel) {
		t.Fatalf("Read returned %v, want ErrSidebandChannel", err)
	}
	if _, err := reader.Read(make([]byte, 16)); !errors.Is(err, ErrSidebandChannel) {
		t.Fatalf("Read after the error returned %v, want the same ErrSidebandChannel", err)
	}
}

func TestSidebandReaderPropagatesDecodeError(t *testing.T) {
	reader := NewSidebandReader(bytes.NewReader([]byte("zzzz")), nil)
	_, err := reader.Read(make([]byte, 16))
	if !errors.Is(err, ErrPktLineBadLength) {
		t.Fatalf("Read returned %v, want ErrPktLineBadLength", err)
	}
}

func TestSidebandReaderCleanEOFWithoutFlush(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).WriteData([]byte{SidebandPack, 'x'}); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
	reader := NewSidebandReader(&buf, nil)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	if string(got) != "x" {
		t.Fatalf("ReadAll returned %q, want %q", got, "x")
	}
}

func TestSidebandWriterChunksLargePayloads(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, minPktLength+1+4)
	payload := bytes.Repeat([]byte{0x42}, 10)
	if err := mux.WritePack(payload); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatalf("Flush returned error %v", err)
	}

	reader := NewSidebandReader(&buf, nil)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("ReadAll returned %v, want %v", got, payload)
	}
}

func TestSidebandWriterZeroLengthChannel(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, 0)
	if err := mux.WritePack(nil); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatalf("Flush returned error %v", err)
	}
	dec := NewDecoder(&buf)
	if !dec.Scan() {
		t.Fatalf("Scan returned false, Err() = %v", dec.Err())
	}
	if len(dec.Bytes()) != 1 || dec.Bytes()[0] != SidebandPack {
		t.Fatalf("Bytes() = %v, want a single pack-channel byte", dec.Bytes())
	}
}

func TestSidebandWriterRejectsUndersizedPacket(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, minPktLength)
	if err := mux.WritePack([]byte("x")); !errors.Is(err, ErrPktLineTooLong) {
		t.Fatalf("WritePack returned %v, want ErrPktLineTooLong", err)
	}
}

func TestSidebandWriterPropagatesEncodeError(t *testing.T) {
	mux := NewSidebandWriter(failingWriter{}, 0)
	if err := mux.WritePack([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("WritePack returned %v, want io.ErrClosedPipe", err)
	}
	if err := mux.Flush(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Flush returned %v, want io.ErrClosedPipe", err)
	}
}

func TestSidebandReaderReadAfterDoneReturnsEOF(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	reader := NewSidebandReader(&buf, nil)
	if _, err := reader.Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("first Read returned %v, want io.EOF", err)
	}
	if _, err := reader.Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("second Read returned %v, want io.EOF", err)
	}
}

func TestSidebandWriterCapsPacketSizeAboveMaximum(t *testing.T) {
	var buf bytes.Buffer
	mux := NewSidebandWriter(&buf, SidebandLargePacket*2)
	if err := mux.WritePack(make([]byte, 10)); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if mux.maxPacket != SidebandLargePacket {
		t.Fatalf("maxPacket = %d, want %d", mux.maxPacket, SidebandLargePacket)
	}
}
