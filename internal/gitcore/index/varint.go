package index

import (
	"fmt"
	"math"
)

const varintBufferSize = 16

func appendVarint(dst []byte, value uint64) []byte {
	var buf [varintBufferSize]byte
	pos := len(buf) - 1
	buf[pos] = byte(value & 127)
	for {
		value >>= 7
		if value == 0 {
			break
		}
		value--
		pos--
		buf[pos] = 128 | byte(value&127)
	}
	return append(dst, buf[pos:]...)
}

func decodeVarint(data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("%w: a variable width number needs at least one byte", ErrTruncated)
	}
	current := data[0]
	value := uint64(current & 127)
	read := 1
	for current&128 != 0 {
		if read == len(data) {
			return 0, 0, fmt.Errorf("%w: a variable width number stops after %d bytes", ErrTruncated, read)
		}
		if value >= math.MaxUint64>>7 {
			return 0, 0, fmt.Errorf("%w: a variable width number does not fit into 64 bits", ErrMalformed)
		}
		value++
		current = data[read]
		read++
		value = value<<7 + uint64(current&127)
	}
	return value, read, nil
}
