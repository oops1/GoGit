package transport

import (
	"bytes"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/progress"
)

const (
	SidebandPack     = 1
	SidebandProgress = 2
	SidebandError    = 3
)

const (
	SidebandSmallPacket = 1000
	SidebandLargePacket = maxPktLength
)

type SidebandReader struct {
	dec      *Decoder
	progress progress.Func
	pending  []byte
	err      error
	done     bool
}

func NewSidebandReader(r io.Reader, prog progress.Func) *SidebandReader {
	return &SidebandReader{dec: NewDecoder(r), progress: prog}
}

func (s *SidebandReader) Read(p []byte) (int, error) {
	for {
		if len(s.pending) > 0 {
			n := copy(p, s.pending)
			s.pending = s.pending[n:]
			return n, nil
		}
		if s.err != nil {
			return 0, s.err
		}
		if s.done {
			return 0, io.EOF
		}
		if !s.dec.Scan() {
			if err := s.dec.Err(); err != nil {
				s.err = err
				return 0, err
			}
			s.done = true
			return 0, io.EOF
		}
		switch s.dec.Type() {
		case PktFlush, PktResponseEnd:
			s.done = true
			return 0, io.EOF
		case PktDelim:
			continue
		case PktData:
			line := s.dec.Bytes()
			if len(line) == 0 {
				continue
			}
			if err := s.consume(line[0], line[1:]); err != nil {
				return 0, err
			}
		}
	}
}

func (s *SidebandReader) consume(channel byte, payload []byte) error {
	switch channel {
	case SidebandPack:
		s.pending = append(s.pending[:0], payload...)
		return nil
	case SidebandProgress:
		s.progress.Message(string(payload))
		return nil
	case SidebandError:
		s.err = fmt.Errorf("%w: %s", ErrSidebandRemote, bytes.TrimRight(payload, "\n"))
		return s.err
	default:
		s.err = fmt.Errorf("%w: %d", ErrSidebandChannel, channel)
		return s.err
	}
}

type SidebandWriter struct {
	enc       *Encoder
	maxPacket int
}

func NewSidebandWriter(w io.Writer, maxPacket int) *SidebandWriter {
	if maxPacket <= 0 || maxPacket > SidebandLargePacket {
		maxPacket = SidebandLargePacket
	}
	return &SidebandWriter{enc: NewEncoder(w), maxPacket: maxPacket}
}

func (m *SidebandWriter) WritePack(data []byte) error {
	return m.writeChannel(SidebandPack, data)
}

func (m *SidebandWriter) WriteProgress(data []byte) error {
	return m.writeChannel(SidebandProgress, data)
}

func (m *SidebandWriter) WriteError(data []byte) error {
	return m.writeChannel(SidebandError, data)
}

func (m *SidebandWriter) Flush() error {
	return m.enc.WriteFlush()
}

func (m *SidebandWriter) writeChannel(channel byte, data []byte) error {
	maxChunk := m.maxPacket - minPktLength - 1
	if maxChunk <= 0 {
		return fmt.Errorf("%w: packet size %d is too small for side-band framing", ErrPktLineTooLong, m.maxPacket)
	}
	if len(data) == 0 {
		return m.enc.WriteData([]byte{channel})
	}
	for len(data) > 0 {
		chunkLength := min(len(data), maxChunk)
		frame := make([]byte, chunkLength+1)
		frame[0] = channel
		copy(frame[1:], data[:chunkLength])
		if err := m.enc.WriteData(frame); err != nil {
			return err
		}
		data = data[chunkLength:]
	}
	return nil
}
