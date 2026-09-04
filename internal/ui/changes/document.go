package changes

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/diffview"
)

const MaxLinesPerFile = 5000

const noNewlineText = "No newline at end of file"

func FromFile(f diff.File) diffview.Document {
	doc := diffview.Document{OldName: f.OldPath, NewName: f.NewPath, Binary: f.Binary}
	if f.Binary {
		return doc
	}
	remaining := MaxLinesPerFile
	for _, h := range f.Hunks {
		if remaining <= 0 {
			doc.Hunks = append(doc.Hunks, truncationHunk())
			break
		}
		hunk, used := fromHunk(h, remaining)
		doc.Hunks = append(doc.Hunks, hunk)
		remaining -= used
		if used < len(h.Lines) {
			doc.Hunks = append(doc.Hunks, truncationHunk())
			break
		}
	}
	return doc
}

func truncationHunk() diffview.Hunk {
	return diffview.Hunk{Header: i18n.T("Diff.Truncated")}
}

func fromHunk(h diff.Hunk, budget int) (diffview.Hunk, int) {
	limit := min(len(h.Lines), budget)
	lines := h.Lines[:limit]
	spans := inlineSpansFor(lines)
	out := make([]diffview.Line, 0, limit)
	oldNo, newNo := h.OldStart, h.NewStart
	for i, l := range lines {
		line := diffview.Line{Text: l.Text, Spans: spans[i]}
		switch l.Kind {
		case diff.KindContext:
			line.Kind = diffview.Context
			line.OldNo, line.NewNo = oldNo, newNo
			oldNo++
			newNo++
		case diff.KindAdd:
			line.Kind = diffview.Added
			line.NewNo = newNo
			newNo++
		case diff.KindDel:
			line.Kind = diffview.Removed
			line.OldNo = oldNo
			oldNo++
		}
		out = append(out, line)
		if l.NoNewline {
			out = append(out, diffview.Line{Kind: diffview.NoNewline, Text: noNewlineText})
		}
	}
	return diffview.Hunk{Header: hunkBanner(h), Lines: out}, limit
}

func hunkBanner(h diff.Hunk) string {
	var b strings.Builder
	b.WriteString("@@ -")
	writeHunkRange(&b, h.OldStart, h.OldLines)
	b.WriteString(" +")
	writeHunkRange(&b, h.NewStart, h.NewLines)
	b.WriteString(" @@")
	if h.Header != "" {
		b.WriteByte(' ')
		b.WriteString(h.Header)
	}
	return b.String()
}

func writeHunkRange(b *strings.Builder, start, count int) {
	if count == 0 {
		start--
	}
	b.WriteString(strconv.Itoa(start))
	if count != 1 {
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(count))
	}
}

func inlineSpansFor(lines []diff.Line) [][]diffview.Span {
	spans := make([][]diffview.Span, len(lines))
	i := 0
	for i < len(lines) {
		if lines[i].Kind != diff.KindDel {
			i++
			continue
		}
		delStart := i
		for i < len(lines) && lines[i].Kind == diff.KindDel {
			i++
		}
		addStart := i
		for i < len(lines) && lines[i].Kind == diff.KindAdd {
			i++
		}
		pairInlineSpans(lines, delStart, addStart, i, spans)
	}
	return spans
}

func pairInlineSpans(lines []diff.Line, delStart, addStart, end int, spans [][]diffview.Span) {
	pairs := min(addStart-delStart, end-addStart)
	for i := range pairs {
		oldSpans, newSpans := inlineSpanPair(lines[delStart+i].Text, lines[addStart+i].Text)
		spans[delStart+i] = oldSpans
		spans[addStart+i] = newSpans
	}
}

func inlineSpanPair(oldText, newText string) ([]diffview.Span, []diffview.Span) {
	var oldSpans, newSpans []diffview.Span
	oldOffset, newOffset := 0, 0
	for _, span := range diff.InlineDiff(oldText, newText) {
		length := utf8.RuneCountInString(span.Text)
		switch span.Kind {
		case diff.KindDel:
			oldSpans = append(oldSpans, diffview.Span{Start: oldOffset, End: oldOffset + length})
			oldOffset += length
		case diff.KindAdd:
			newSpans = append(newSpans, diffview.Span{Start: newOffset, End: newOffset + length})
			newOffset += length
		case diff.KindContext:
			oldOffset += length
			newOffset += length
		}
	}
	return oldSpans, newSpans
}
