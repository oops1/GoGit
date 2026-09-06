package transport

import (
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	MaxDataLength = 65516
	minPktLength  = 4
	maxPktLength  = minPktLength + MaxDataLength
)

type PktType int

const (
	PktData PktType = iota
	PktFlush
	PktDelim
	PktResponseEnd
)

func (t PktType) String() string {
	switch t {
	case PktData:
		return "data"
	case PktFlush:
		return "flush"
	case PktDelim:
		return "delim"
	case PktResponseEnd:
		return "response-end"
	default:
		return "unknown"
	}
}

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) WriteData(data []byte) error {
	if len(data) > MaxDataLength {
		return fmt.Errorf("%w: %d bytes", ErrPktLineTooLong, len(data))
	}
	var header [minPktLength]byte
	encodePktLength(header[:], len(data)+minPktLength)
	if _, err := e.w.Write(header[:]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := e.w.Write(data)
	return err
}

func (e *Encoder) WriteFlush() error {
	return e.writeMarker(0)
}

func (e *Encoder) WriteDelim() error {
	return e.writeMarker(1)
}

func (e *Encoder) WriteResponseEnd() error {
	return e.writeMarker(2)
}

func (e *Encoder) writeMarker(length int) error {
	var header [minPktLength]byte
	encodePktLength(header[:], length)
	_, err := e.w.Write(header[:])
	return err
}

func encodePktLength(dst []byte, length int) {
	const hexDigits = "0123456789abcdef"
	dst[0] = hexDigits[(length>>12)&0xf]
	dst[1] = hexDigits[(length>>8)&0xf]
	dst[2] = hexDigits[(length>>4)&0xf]
	dst[3] = hexDigits[length&0xf]
}

type Decoder struct {
	r   io.Reader
	buf []byte
	typ PktType
	err error
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r, buf: make([]byte, 0, MaxDataLength)}
}

func (d *Decoder) Scan() bool {
	if d.err != nil {
		return false
	}
	var header [minPktLength]byte
	if _, err := io.ReadFull(d.r, header[:]); err != nil {
		if !errors.Is(err, io.EOF) {
			d.err = fmt.Errorf("%w: %w", ErrPktLineTruncated, err)
		}
		return false
	}
	length, err := strconv.ParseUint(string(header[:]), 16, 16)
	if err != nil {
		d.err = fmt.Errorf("%w: %q", ErrPktLineBadLength, string(header[:]))
		return false
	}
	switch length {
	case 0:
		d.typ = PktFlush
		d.buf = d.buf[:0]
		return true
	case 1:
		d.typ = PktDelim
		d.buf = d.buf[:0]
		return true
	case 2:
		d.typ = PktResponseEnd
		d.buf = d.buf[:0]
		return true
	case 3:
		d.err = fmt.Errorf("%w", ErrPktLineReserved)
		return false
	}
	if int(length) > maxPktLength {
		d.err = fmt.Errorf("%w: %d", ErrPktLineTooLong, length)
		return false
	}
	dataLength := int(length) - minPktLength
	d.buf = d.buf[:dataLength]
	if _, err := io.ReadFull(d.r, d.buf); err != nil {
		d.err = fmt.Errorf("%w: %w", ErrPktLineTruncated, err)
		return false
	}
	d.typ = PktData
	return true
}

func (d *Decoder) Type() PktType {
	return d.typ
}

func (d *Decoder) Bytes() []byte {
	return d.buf
}

func (d *Decoder) Err() error {
	return d.err
}
