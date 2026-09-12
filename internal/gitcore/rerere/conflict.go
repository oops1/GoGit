package rerere

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"hash"
)

const DefaultMarkerSize = 7

const (
	oursMarker   = '<'
	baseMarker   = '|'
	middleMarker = '='
	theirsMarker = '>'
)

type Conflict struct {
	ID       string
	Preimage []byte
}

type side int

const (
	sideOurs side = iota
	sideBase
	sideTheirs
)

func Normalize(data []byte, markerSize int) (Conflict, bool) {
	if markerSize <= 0 {
		markerSize = DefaultMarkerSize
	}
	lines := splitLines(data)
	digest := sha1.New()
	var out bytes.Buffer
	found := false
	for at := 0; at < len(lines); {
		if !isMarker(lines[at], oursMarker, markerSize) {
			out.Write(lines[at])
			at++
			continue
		}
		hunk, next, ok := readConflict(lines, at+1, markerSize, digest)
		if !ok {
			return Conflict{}, false
		}
		out.Write(hunk)
		at = next
		found = true
	}
	if !found {
		return Conflict{}, false
	}
	return Conflict{ID: hex.EncodeToString(digest.Sum(nil)), Preimage: out.Bytes()}, true
}

func Resolved(data []byte, markerSize int) bool {
	_, conflicted := Normalize(data, markerSize)
	return !conflicted
}

func readConflict(lines [][]byte, at, markerSize int, digest hash.Hash) ([]byte, int, bool) {
	var ours, theirs bytes.Buffer
	where := sideOurs
	for at < len(lines) {
		line := lines[at]
		switch {
		case isMarker(line, oursMarker, markerSize):
			nested, next, ok := readConflict(lines, at+1, markerSize, nil)
			if !ok {
				return nil, at, false
			}
			collect(&ours, &theirs, where, nested)
			at = next
			continue
		case isMarker(line, baseMarker, markerSize):
			if where != sideOurs {
				return nil, at, false
			}
			where = sideBase
		case isMarker(line, middleMarker, markerSize):
			if where == sideTheirs {
				return nil, at, false
			}
			where = sideTheirs
		case isMarker(line, theirsMarker, markerSize):
			if where != sideTheirs {
				return nil, at, false
			}
			return render(ours.Bytes(), theirs.Bytes(), markerSize, digest), at + 1, true
		default:
			collect(&ours, &theirs, where, line)
		}
		at++
	}
	return nil, at, false
}

func collect(ours, theirs *bytes.Buffer, where side, line []byte) {
	switch where {
	case sideOurs:
		ours.Write(line)
	case sideTheirs:
		theirs.Write(line)
	case sideBase:
	}
}

func render(ours, theirs []byte, markerSize int, digest hash.Hash) []byte {
	if bytes.Compare(ours, theirs) > 0 {
		ours, theirs = theirs, ours
	}
	var out bytes.Buffer
	out.Write(marker(oursMarker, markerSize))
	out.Write(ours)
	out.Write(marker(middleMarker, markerSize))
	out.Write(theirs)
	out.Write(marker(theirsMarker, markerSize))
	if digest != nil {
		_, _ = digest.Write(append(bytes.Clone(ours), 0))
		_, _ = digest.Write(append(bytes.Clone(theirs), 0))
	}
	return out.Bytes()
}

func marker(sign byte, markerSize int) []byte {
	return append(bytes.Repeat([]byte{sign}, markerSize), '\n')
}

func isMarker(line []byte, sign byte, markerSize int) bool {
	if len(line) <= markerSize {
		return false
	}
	for i := range markerSize {
		if line[i] != sign {
			return false
		}
	}
	rest := line[markerSize]
	if sign == oursMarker || sign == theirsMarker {
		return rest == ' '
	}
	return isSpace(rest)
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return append(lines, data)
		}
		lines = append(lines, data[:end+1])
		data = data[end+1:]
	}
	return lines
}
