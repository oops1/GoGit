package linelog

import (
	"bytes"
	"io"
	"strconv"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func filesOf(ranges rangeList) []File {
	var files []File
	for _, entry := range ranges {
		if entry.pair == nil {
			continue
		}
		files = append(files, File{
			OldPath: entry.pair.oldPath,
			NewPath: entry.pair.newPath,
			Created: !entry.pair.oldValid,
			Hunks:   hunksOf(entry),
		})
	}
	return files
}

func hunksOf(entry *fileRanges) []Hunk {
	parent, target := newText(entry.pair.oldData), newText(entry.pair.newData)
	d := entry.touched
	var hunks []Hunk
	j := 0
	for _, r := range entry.ranges {
		if j == len(d.target) || d.target[j].Start > r.End {
			continue
		}
		last := j
		for last < len(d.target) && d.target[last].Start < r.End {
			last++
		}
		if last > j {
			last--
		}
		pStart, pEnd := d.parent[j].Start, d.parent[last].End
		if r.Start < d.target[j].Start {
			pStart -= d.target[j].Start - r.Start
		}
		if r.End > d.target[last].End {
			pEnd += r.End - d.target[last].End
		}
		if pStart == 0 && pEnd == 0 {
			pStart, pEnd = -1, -1
		}
		hunk := Hunk{OldStart: pStart + 1, OldLines: pEnd - pStart, NewStart: r.Start + 1, NewLines: r.End - r.Start}
		cur := r.Start
		for j < len(d.target) && d.target[j].Start < r.End {
			for ; cur < d.target[j].Start; cur++ {
				hunk.Lines = append(hunk.Lines, lineOf(target, cur, diff.KindContext))
			}
			for k := d.parent[j].Start; k < d.parent[j].End; k++ {
				hunk.Lines = append(hunk.Lines, lineOf(parent, k, diff.KindDel))
			}
			for ; cur < d.target[j].End && cur < r.End; cur++ {
				hunk.Lines = append(hunk.Lines, lineOf(target, cur, diff.KindAdd))
			}
			j++
		}
		for ; cur < r.End; cur++ {
			hunk.Lines = append(hunk.Lines, lineOf(target, cur, diff.KindContext))
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}

func lineOf(t *text, at int, kind diff.Kind) diff.Line {
	raw := t.line(at)
	text, newline := bytes.CutSuffix(raw, []byte{'\n'})
	return diff.Line{Kind: kind, Text: string(text), NoNewline: !newline}
}

func (e *Entry) WritePatch(w io.Writer) error {
	var buf bytes.Buffer
	buf.WriteByte('\n')
	for _, file := range e.Files {
		buf.WriteString("diff --git a/" + file.OldPath + " b/" + file.NewPath + "\n")
		if file.Created {
			buf.WriteString("--- /dev/null\n")
		} else {
			buf.WriteString("--- a/" + file.OldPath + "\n")
		}
		buf.WriteString("+++ b/" + file.NewPath + "\n")
		for _, hunk := range file.Hunks {
			writeHunk(&buf, hunk)
		}
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func writeHunk(buf *bytes.Buffer, hunk Hunk) {
	buf.WriteString("@@ -" + strconv.Itoa(hunk.OldStart) + "," + strconv.Itoa(hunk.OldLines))
	buf.WriteString(" +" + strconv.Itoa(hunk.NewStart) + "," + strconv.Itoa(hunk.NewLines) + " @@\n")
	for _, line := range hunk.Lines {
		switch line.Kind {
		case diff.KindAdd:
			buf.WriteByte('+')
		case diff.KindDel:
			buf.WriteByte('-')
		default:
			buf.WriteByte(' ')
		}
		buf.WriteString(line.Text)
		buf.WriteByte('\n')
		if line.NoNewline {
			buf.WriteString("\\ No newline at end of file\n")
		}
	}
}
