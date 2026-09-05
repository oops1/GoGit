package pack

import (
	"encoding/binary"
	"fmt"
	"sync"
)

const (
	copyOpcode      = 0x80
	copyOffsetBytes = 4
	copySizeBytes   = 3
	copySizeShift   = 4
	defaultCopySize = 0x10000

	deltaHashLength    = 4
	deltaMaxCopy       = 0xffffff
	deltaMaxInsert     = 127
	deltaMaxCandidates = 16
)

func ApplyDelta(base, delta []byte) ([]byte, error) {
	sourceSize, read, err := decodeDeltaSize(delta, "source")
	if err != nil {
		return nil, err
	}
	position := read
	targetSize, read, err := decodeDeltaSize(delta[position:], "target")
	if err != nil {
		return nil, err
	}
	position += read
	if sourceSize != int64(len(base)) {
		return nil, fmt.Errorf("%w: base holds %d bytes, the delta expects %d", ErrInvalidDelta, len(base), sourceSize)
	}
	out := make([]byte, 0, min(targetSize, maxPrealloc))
	for position < len(delta) {
		opcode := delta[position]
		position++
		switch {
		case opcode == 0:
			return nil, fmt.Errorf("%w: reserved opcode at %d", ErrInvalidDelta, position-1)
		case opcode&copyOpcode != 0:
			out, position, err = applyCopy(out, base, delta, position, opcode, targetSize)
		default:
			out, position, err = applyInsert(out, delta, position, int(opcode), targetSize)
		}
		if err != nil {
			return nil, err
		}
	}
	if int64(len(out)) != targetSize {
		return nil, fmt.Errorf("%w: %d bytes instead of %d", ErrDeltaSizeMismatch, len(out), targetSize)
	}
	return out[:len(out):len(out)], nil
}

func applyCopy(out, base, delta []byte, position int, opcode byte, limit int64) ([]byte, int, error) {
	var offset, size uint32
	for i := range copyOffsetBytes {
		if opcode&(1<<uint(i)) == 0 {
			continue
		}
		if position >= len(delta) {
			return nil, position, fmt.Errorf("%w: truncated copy offset", ErrInvalidDelta)
		}
		offset |= uint32(delta[position]) << (8 * uint(i))
		position++
	}
	for i := range copySizeBytes {
		if opcode&(1<<uint(copySizeShift+i)) == 0 {
			continue
		}
		if position >= len(delta) {
			return nil, position, fmt.Errorf("%w: truncated copy size", ErrInvalidDelta)
		}
		size |= uint32(delta[position]) << (8 * uint(i))
		position++
	}
	if size == 0 {
		size = defaultCopySize
	}
	end := int64(offset) + int64(size)
	if end > int64(len(base)) {
		return nil, position, fmt.Errorf("%w: copy of %d bytes at %d leaves the %d byte base",
			ErrInvalidDelta, size, offset, len(base))
	}
	if int64(len(out))+int64(size) > limit {
		return nil, position, fmt.Errorf("%w: copy overruns the declared target size %d", ErrInvalidDelta, limit)
	}
	return append(out, base[offset:end]...), position, nil
}

func applyInsert(out, delta []byte, position, size int, limit int64) ([]byte, int, error) {
	if position+size > len(delta) {
		return nil, position, fmt.Errorf("%w: truncated insert of %d bytes", ErrInvalidDelta, size)
	}
	if int64(len(out)+size) > limit {
		return nil, position, fmt.Errorf("%w: insert overruns the declared target size %d", ErrInvalidDelta, limit)
	}
	return append(out, delta[position:position+size]...), position + size, nil
}

func decodeDeltaSize(delta []byte, role string) (int64, int, error) {
	var size int64
	var shift uint
	for i, current := range delta {
		if shift >= 64 {
			return 0, 0, fmt.Errorf("%w: %s size does not fit in 64 bits", ErrInvalidDelta, role)
		}
		size |= int64(current&payloadMask) << shift
		if current&continuation == 0 {
			if size < 0 {
				return 0, 0, fmt.Errorf("%w: negative %s size", ErrInvalidDelta, role)
			}
			return size, i + 1, nil
		}
		shift += payloadBits
	}
	return 0, 0, fmt.Errorf("%w: truncated %s size", ErrInvalidDelta, role)
}

type payload struct {
	data []byte
}

var payloads = sync.Pool{New: func() any { return new(payload) }}

func acquirePayload(size int64) *payload {
	buffer := payloads.Get().(*payload)
	if int64(cap(buffer.data)) < size {
		buffer.data = make([]byte, size)
		return buffer
	}
	buffer.data = buffer.data[:size]
	return buffer
}

func releasePayload(buffer *payload) {
	payloads.Put(buffer)
}

func EncodeDelta(base, target []byte) []byte {
	out := appendDeltaSize(nil, int64(len(base)))
	out = appendDeltaSize(out, int64(len(target)))
	index := buildDeltaIndex(base)
	var pending []byte
	position := 0
	for position < len(target) {
		if matchOffset, length, ok := findDeltaMatch(index, base, target, position); ok {
			out = appendDeltaInsert(out, pending)
			pending = pending[:0]
			out = appendDeltaCopies(out, matchOffset, length)
			position += length
			continue
		}
		pending = append(pending, target[position])
		position++
	}
	return appendDeltaInsert(out, pending)
}

func findDeltaMatch(index map[uint32][]int, base, target []byte, position int) (int, int, bool) {
	if position+deltaHashLength > len(target) {
		return 0, 0, false
	}
	key := binary.LittleEndian.Uint32(target[position:])
	positions, ok := index[key]
	if !ok {
		return 0, 0, false
	}
	bestPosition, bestLength := positions[0], deltaMatchLength(base[positions[0]:], target[position:])
	for _, candidate := range positions[1:] {
		length := deltaMatchLength(base[candidate:], target[position:])
		if length > bestLength {
			bestPosition, bestLength = candidate, length
		}
	}
	return bestPosition, bestLength, true
}

func deltaMatchLength(base, target []byte) int {
	limit := min(len(base), len(target))
	length := 0
	for length < limit && base[length] == target[length] {
		length++
	}
	return length
}

func buildDeltaIndex(base []byte) map[uint32][]int {
	if len(base) < deltaHashLength {
		return nil
	}
	index := make(map[uint32][]int)
	for position := 0; position+deltaHashLength <= len(base); position++ {
		key := binary.LittleEndian.Uint32(base[position:])
		if list := index[key]; len(list) < deltaMaxCandidates {
			index[key] = append(list, position)
		}
	}
	return index
}

func appendDeltaSize(out []byte, size int64) []byte {
	for {
		current := byte(size & payloadMask)
		size >>= payloadBits
		if size == 0 {
			return append(out, current)
		}
		out = append(out, current|continuation)
	}
}

func appendDeltaInsert(out, pending []byte) []byte {
	for len(pending) > 0 {
		chunk := min(len(pending), deltaMaxInsert)
		out = append(out, byte(chunk))
		out = append(out, pending[:chunk]...)
		pending = pending[chunk:]
	}
	return out
}

func appendDeltaCopies(out []byte, base, length int) []byte {
	for length > 0 {
		chunk := min(length, deltaMaxCopy)
		out = appendDeltaCopy(out, uint32(base), uint32(chunk))
		base += chunk
		length -= chunk
	}
	return out
}

func appendDeltaCopy(out []byte, offset, size uint32) []byte {
	opcode := byte(copyOpcode)
	var tail [copyOffsetBytes + copySizeBytes]byte
	written := 0
	for i := range copyOffsetBytes {
		current := byte(offset >> (8 * uint(i)))
		if current == 0 {
			continue
		}
		opcode |= 1 << uint(i)
		tail[written] = current
		written++
	}
	encoded := size
	if encoded == defaultCopySize {
		encoded = 0
	}
	for i := range copySizeBytes {
		current := byte(encoded >> (8 * uint(i)))
		if current == 0 {
			continue
		}
		opcode |= 1 << uint(copySizeShift+i)
		tail[written] = current
		written++
	}
	out = append(out, opcode)
	return append(out, tail[:written]...)
}
