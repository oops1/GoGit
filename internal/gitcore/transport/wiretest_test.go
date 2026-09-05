package transport

import (
	"bytes"
)

type pktBuilder struct {
	buf bytes.Buffer
}

func newPktBuilder() *pktBuilder {
	return &pktBuilder{}
}

func (b *pktBuilder) line(text string) *pktBuilder {
	if err := NewEncoder(&b.buf).WriteData([]byte(text)); err != nil {
		panic(err)
	}
	return b
}

func (b *pktBuilder) flush() *pktBuilder {
	if err := NewEncoder(&b.buf).WriteFlush(); err != nil {
		panic(err)
	}
	return b
}

func (b *pktBuilder) delim() *pktBuilder {
	if err := NewEncoder(&b.buf).WriteDelim(); err != nil {
		panic(err)
	}
	return b
}

func (b *pktBuilder) raw(data []byte) *pktBuilder {
	b.buf.Write(data)
	return b
}

func (b *pktBuilder) bytes() []byte {
	return b.buf.Bytes()
}

func sidebandFrame(channel byte, data []byte) []byte {
	frame := append([]byte{channel}, data...)
	var out bytes.Buffer
	if err := NewEncoder(&out).WriteData(frame); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func buildSidebandPack(pack []byte) []byte {
	var out bytes.Buffer
	out.Write(sidebandFrame(SidebandPack, pack))
	if err := NewEncoder(&out).WriteFlush(); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func findRefByName(refs []Ref, name string) (Ref, bool) {
	for _, ref := range refs {
		if ref.Name == name {
			return ref, true
		}
	}
	return Ref{}, false
}

func fakePack(seed byte) []byte {
	data := make([]byte, 0, 12+8)
	data = append(data, "PACK"...)
	data = append(data, 0, 0, 0, 2)
	data = append(data, 0, 0, 0, byte(seed))
	data = append(data, seed, seed+1, seed+2, seed+3)
	return data
}
