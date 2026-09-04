package diffview

import (
	"errors"
	"fmt"
	"iter"
	"strconv"
	"strings"
)

var (
	ErrMultipleFiles   = errors.New("diffview: unified diff describes more than one file")
	ErrHunkHeader      = errors.New("diffview: malformed hunk header")
	ErrLineOutsideHunk = errors.New("diffview: diff line before the first hunk header")
)

const devNull = "/dev/null"

func ParseUnified(data []byte) (Document, error) {
	var doc Document
	var oldNo, newNo int
	fileSeen := false
	inHunk := false

	for text := range unifiedLines(data) {
		switch {
		case strings.HasPrefix(text, "diff --git "):
			if fileSeen {
				return Document{}, ErrMultipleFiles
			}
			fileSeen = true
			inHunk = false
			doc.OldName, doc.NewName = gitHeaderNames(text)

		case !inHunk && strings.HasPrefix(text, "--- "):
			fileSeen = true
			doc.OldName = headerPath(text[4:])

		case !inHunk && strings.HasPrefix(text, "+++ "):
			fileSeen = true
			doc.NewName = headerPath(text[4:])

		case strings.HasPrefix(text, "@@"):
			hunk, oldStart, newStart, err := parseHunkHeader(text)
			if err != nil {
				return Document{}, err
			}
			fileSeen = true
			inHunk = true
			oldNo, newNo = oldStart, newStart
			doc.Hunks = append(doc.Hunks, hunk)

		case strings.HasPrefix(text, "Binary files ") || text == "GIT binary patch":
			fileSeen = true
			inHunk = false
			doc.Binary = true

		case strings.HasPrefix(text, `\`):
			if !inHunk {
				return Document{}, ErrLineOutsideHunk
			}
			appendLine(&doc, Line{Kind: NoNewline, Text: strings.TrimSpace(text[1:])})

		case text == "" || text[0] == ' ':
			if !inHunk {
				continue
			}
			appendLine(&doc, Line{Kind: Context, OldNo: oldNo, NewNo: newNo, Text: trimMarker(text)})
			oldNo++
			newNo++

		case text[0] == '+':
			if !inHunk {
				return Document{}, ErrLineOutsideHunk
			}
			appendLine(&doc, Line{Kind: Added, NewNo: newNo, Text: text[1:]})
			newNo++

		case text[0] == '-':
			if !inHunk {
				return Document{}, ErrLineOutsideHunk
			}
			appendLine(&doc, Line{Kind: Removed, OldNo: oldNo, Text: text[1:]})
			oldNo++

		default:
			inHunk = false
		}
	}

	markSpans(&doc)
	return doc, nil
}

func unifiedLines(data []byte) iter.Seq[string] {
	return func(yield func(string) bool) {
		rest := string(data)
		for rest != "" {
			text, tail, found := strings.Cut(rest, "\n")
			if !found {
				yield(text)
				return
			}
			if !yield(text) {
				return
			}
			rest = tail
		}
	}
}

func trimMarker(text string) string {
	if text == "" {
		return ""
	}
	return text[1:]
}

func appendLine(doc *Document, line Line) {
	hunk := &doc.Hunks[len(doc.Hunks)-1]
	hunk.Lines = append(hunk.Lines, line)
}

func gitHeaderNames(text string) (string, string) {
	rest := strings.TrimPrefix(text, "diff --git ")
	oldPath, newPath, found := strings.Cut(rest, " b/")
	if !found {
		return headerPath(rest), headerPath(rest)
	}
	return headerPath(oldPath), newPath
}

func headerPath(text string) string {
	path, _, _ := strings.Cut(text, "\t")
	path = strings.TrimSpace(path)
	if path == devNull {
		return ""
	}
	if trimmed, ok := strings.CutPrefix(path, "a/"); ok {
		return trimmed
	}
	if trimmed, ok := strings.CutPrefix(path, "b/"); ok {
		return trimmed
	}
	return path
}

func parseHunkHeader(text string) (Hunk, int, int, error) {
	body, found := strings.CutPrefix(text, "@@ ")
	if !found {
		return Hunk{}, 0, 0, fmt.Errorf("%w: %s", ErrHunkHeader, text)
	}
	ranges, _, found := strings.Cut(body, " @@")
	if !found {
		return Hunk{}, 0, 0, fmt.Errorf("%w: %s", ErrHunkHeader, text)
	}
	oldPart, newPart, found := strings.Cut(ranges, " ")
	if !found {
		return Hunk{}, 0, 0, fmt.Errorf("%w: %s", ErrHunkHeader, text)
	}
	oldStart, err := rangeStart(oldPart, "-")
	if err != nil {
		return Hunk{}, 0, 0, fmt.Errorf("%w: %s", err, text)
	}
	newStart, err := rangeStart(newPart, "+")
	if err != nil {
		return Hunk{}, 0, 0, fmt.Errorf("%w: %s", err, text)
	}
	return Hunk{Header: text}, oldStart, newStart, nil
}

func rangeStart(part, sign string) (int, error) {
	digits, found := strings.CutPrefix(part, sign)
	if !found {
		return 0, ErrHunkHeader
	}
	start, _, _ := strings.Cut(digits, ",")
	value, err := strconv.Atoi(start)
	if err != nil || value < 0 {
		return 0, ErrHunkHeader
	}
	return value, nil
}

func markSpans(doc *Document) {
	for hi := range doc.Hunks {
		lines := doc.Hunks[hi].Lines
		i := 0
		for i < len(lines) {
			if lines[i].Kind != Removed {
				i++
				continue
			}
			removedStart := i
			for i < len(lines) && lines[i].Kind == Removed {
				i++
			}
			addedStart := i
			for i < len(lines) && lines[i].Kind == Added {
				i++
			}
			pairSpans(lines[removedStart:addedStart], lines[addedStart:i])
		}
	}
}

func pairSpans(removed, added []Line) {
	for i := 0; i < len(removed) && i < len(added); i++ {
		oldSpan, newSpan, ok := changedSpans(removed[i].Text, added[i].Text)
		if !ok {
			continue
		}
		removed[i].Spans = oldSpan
		added[i].Spans = newSpan
	}
}

func changedSpans(oldText, newText string) ([]Span, []Span, bool) {
	oldRunes := []rune(oldText)
	newRunes := []rune(newText)
	prefix := 0
	for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldRunes)-prefix && suffix < len(newRunes)-prefix &&
		oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
		suffix++
	}
	if prefix == 0 && suffix == 0 {
		return nil, nil, false
	}
	oldEnd := len(oldRunes) - suffix
	newEnd := len(newRunes) - suffix
	if prefix >= oldEnd && prefix >= newEnd {
		return nil, nil, false
	}
	var oldSpans, newSpans []Span
	if prefix < oldEnd {
		oldSpans = []Span{{Start: prefix, End: oldEnd}}
	}
	if prefix < newEnd {
		newSpans = []Span{{Start: prefix, End: newEnd}}
	}
	return oldSpans, newSpans, true
}
