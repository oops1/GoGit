package pack

import (
	"fmt"
	"sync"
)

const (
	copyOpcode      = 0x80
	copyOffsetBytes = 4
	copySizeBytes   = 3
	copySizeShift   = 4
	defaultCopySize = 0x10000
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
	return out, nil
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
