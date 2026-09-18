package commitgraph

import (
	"encoding/binary"
	"math/bits"
	"strings"
)

const (
	bloomHashVersion    = 1
	bloomNumHashes      = 7
	bloomBitsPerEntry   = 10
	bloomMaxChanges     = 512
	bloomHeaderSize     = 12
	bloomSeedFirst      = 0x293ae76f
	bloomSeedSecond     = 0x7e646e2c
	bloomLargeFilter    = 0xff
	bitsPerByte         = 8
	murmurC1            = 0xcc9e2d51
	murmurC2            = 0x1b873593
	murmurR1            = 15
	murmurR2            = 13
	murmurM             = 5
	murmurN             = 0xe6546b64
	murmurFinalFirst    = 0x85ebca6b
	murmurFinalSecond   = 0xc2b2ae35
	murmurBlockSize     = 4
	murmurTailThirdByte = 16
	murmurTailByteShift = 8
)

type bloomSettings struct {
	hashVersion  uint32
	numHashes    uint32
	bitsPerEntry uint32
}

type BloomKey struct {
	hashes []uint32
}

type ChangedPaths struct {
	Paths []string
	Large bool
}

func signedByte(c byte) uint32 {
	return uint32(int32(int8(c)))
}

func murmur3(seed uint32, data string) uint32 {
	blocks := len(data) / murmurBlockSize
	for at := range blocks {
		offset := at * murmurBlockSize
		k := signedByte(data[offset]) | signedByte(data[offset+1])<<8 | signedByte(data[offset+2])<<16 | signedByte(data[offset+3])<<24
		k *= murmurC1
		k = bits.RotateLeft32(k, murmurR1)
		k *= murmurC2
		seed ^= k
		seed = bits.RotateLeft32(seed, murmurR2)*murmurM + murmurN
	}
	tail := data[blocks*murmurBlockSize:]
	var k uint32
	switch len(tail) {
	case 3:
		k ^= signedByte(tail[2]) << murmurTailThirdByte
		fallthrough
	case 2:
		k ^= signedByte(tail[1]) << murmurTailByteShift
		fallthrough
	case 1:
		k ^= signedByte(tail[0])
		k *= murmurC1
		k = bits.RotateLeft32(k, murmurR1)
		k *= murmurC2
		seed ^= k
	}
	seed ^= uint32(len(data))
	seed ^= seed >> 16
	seed *= murmurFinalFirst
	seed ^= seed >> 13
	seed *= murmurFinalSecond
	seed ^= seed >> 16
	return seed
}

func newBloomKey(path string, numHashes uint32) BloomKey {
	first := murmur3(bloomSeedFirst, path)
	second := murmur3(bloomSeedSecond, path)
	key := BloomKey{hashes: make([]uint32, numHashes)}
	for at := range key.hashes {
		key.hashes[at] = first + uint32(at)*second
	}
	return key
}

func pathKeys(path string, numHashes uint32) []BloomKey {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return nil
	}
	keys := []BloomKey{newBloomKey(path, numHashes)}
	for at := len(path) - 1; at > 0; at-- {
		if path[at] == '/' {
			keys = append(keys, newBloomKey(path[:at], numHashes))
		}
	}
	return keys
}

func filterContains(filter []byte, key BloomKey) bool {
	mod := uint64(len(filter)) * bitsPerByte
	for _, h := range key.hashes {
		bit := uint64(h) % mod
		if filter[bit/bitsPerByte]&(1<<(bit%bitsPerByte)) == 0 {
			return false
		}
	}
	return true
}

func buildFilter(changed *ChangedPaths) []byte {
	if changed == nil {
		return nil
	}
	if changed.Large || len(changed.Paths) > bloomMaxChanges {
		return []byte{bloomLargeFilter}
	}
	unique := map[string]struct{}{}
	for _, path := range changed.Paths {
		for path != "" {
			unique[path] = struct{}{}
			cut := strings.LastIndexByte(path, '/')
			if cut < 0 {
				break
			}
			path = path[:cut]
		}
	}
	if len(unique) > bloomMaxChanges {
		return []byte{bloomLargeFilter}
	}
	filter := make([]byte, max((len(unique)*bloomBitsPerEntry+bitsPerByte-1)/bitsPerByte, 1))
	mod := uint64(len(filter)) * bitsPerByte
	for path := range unique {
		for _, h := range newBloomKey(path, bloomNumHashes).hashes {
			bit := uint64(h) % mod
			filter[bit/bitsPerByte] |= 1 << (bit % bitsPerByte)
		}
	}
	return filter
}

func bloomChunks(filters [][]byte) ([]byte, []byte) {
	index := make([]byte, 0, len(filters)*wordSize)
	data := binary.BigEndian.AppendUint32(nil, bloomHashVersion)
	data = binary.BigEndian.AppendUint32(data, bloomNumHashes)
	data = binary.BigEndian.AppendUint32(data, bloomBitsPerEntry)
	end := uint32(0)
	for _, filter := range filters {
		end += uint32(len(filter))
		index = binary.BigEndian.AppendUint32(index, end)
		data = append(data, filter...)
	}
	return index, data
}
