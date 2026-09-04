package diff

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type statRow struct {
	name    string
	added   int
	deleted int
	binary  bool
}

func statRows(files []File) []statRow {
	rows := make([]statRow, 0, len(files))
	for _, file := range files {
		oldPath, newPath := file.paths()
		row := statRow{name: quoteCStyle(newPath), binary: file.Binary}
		if oldPath != newPath {
			row.name = renameName(oldPath, newPath)
		}
		switch {
		case !file.Binary:
			row.added, row.deleted = file.counts()
		case file.OldID != file.NewID:
			row.added, row.deleted = file.NewSize, file.OldSize
		}
		if uninteresting(file, row) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func uninteresting(file File, row statRow) bool {
	return file.Status == StatusModified &&
		!row.binary &&
		row.added == 0 &&
		row.deleted == 0 &&
		file.OldMode == file.NewMode
}

func NumStat(w io.Writer, files []File) error {
	var buf bytes.Buffer
	for _, row := range statRows(files) {
		if row.binary {
			buf.WriteString("-\t-\t")
		} else {
			fmt.Fprintf(&buf, "%d\t%d\t", row.added, row.deleted)
		}
		buf.WriteString(row.name)
		buf.WriteByte('\n')
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func decimalWidth(value int) int {
	width := 1
	for value >= 10 {
		width++
		value /= 10
	}
	return width
}

func scaleLinear(value, width, maxChange int) int {
	if value == 0 {
		return 0
	}
	return 1 + (value * (width - 1) / maxChange)
}

func Stat(w io.Writer, files []File, opts Options) error {
	opts = opts.normalized()
	rows := statRows(files)
	if len(rows) == 0 {
		return nil
	}
	var buf bytes.Buffer
	nameWidth, numberWidth, graphWidth, maxChange := statLayout(rows, opts.StatWidth)
	for _, row := range rows {
		writeStatRow(&buf, row, nameWidth, numberWidth, graphWidth, maxChange)
	}
	writeStatSummary(&buf, rows)
	_, err := w.Write(buf.Bytes())
	return err
}

func statLayout(rows []statRow, width int) (nameWidth, numberWidth, graphWidth, maxChange int) {
	maxLen, binWidth := 0, 0
	for _, row := range rows {
		maxLen = max(maxLen, utf8.RuneCountInString(row.name))
		if row.binary {
			binWidth = max(binWidth, 14+decimalWidth(row.added)+decimalWidth(row.deleted))
			numberWidth = 3
			continue
		}
		maxChange = max(maxChange, row.added+row.deleted)
	}
	numberWidth = max(numberWidth, decimalWidth(maxChange))
	width = max(width, 16+6+numberWidth)

	graphWidth = binWidth - 4
	if maxChange+4 > binWidth {
		graphWidth = maxChange
	}
	nameWidth = maxLen

	if nameWidth+numberWidth+6+graphWidth > width {
		if graphWidth > width*3/8-numberWidth-6 {
			graphWidth = max(width*3/8-numberWidth-6, 6)
		}
		if nameWidth > width-numberWidth-6-graphWidth {
			nameWidth = width - numberWidth - 6 - graphWidth
		} else {
			graphWidth = width - numberWidth - 6 - nameWidth
		}
	}
	return nameWidth, numberWidth, graphWidth, maxChange
}

func shortenName(name string, nameWidth int) (prefix, shortened string, padding int) {
	length := nameWidth
	nameLen := utf8.RuneCountInString(name)
	if nameWidth < nameLen {
		prefix = "..."
		length -= 3
		shortened = name[nameLen-length:]
		if slash := strings.IndexByte(shortened, '/'); slash >= 0 {
			shortened = shortened[slash:]
		}
	} else {
		shortened = name
	}
	padding = max(length-utf8.RuneCountInString(shortened), 0)
	return prefix, shortened, padding
}

func writeStatRow(buf *bytes.Buffer, row statRow, nameWidth, numberWidth, graphWidth, maxChange int) {
	prefix, name, padding := shortenName(row.name, nameWidth)
	if row.binary {
		fmt.Fprintf(buf, " %s%s%*s | %*s", prefix, name, padding, "", numberWidth, "Bin")
		if row.added == 0 && row.deleted == 0 {
			buf.WriteByte('\n')
			return
		}
		fmt.Fprintf(buf, " %d -> %d bytes\n", row.deleted, row.added)
		return
	}

	add, del := row.added, row.deleted
	if graphWidth <= maxChange {
		total := scaleLinear(add+del, graphWidth, maxChange)
		if total < 2 && add > 0 && del > 0 {
			total = 2
		}
		if add < del {
			add = scaleLinear(add, graphWidth, maxChange)
			del = total - add
		} else {
			del = scaleLinear(del, graphWidth, maxChange)
			add = total - del
		}
	}
	trailer := ""
	if row.added+row.deleted > 0 {
		trailer = " "
	}
	fmt.Fprintf(buf, " %s%s%*s | %*d%s", prefix, name, padding, "", numberWidth, row.added+row.deleted, trailer)
	buf.WriteString(strings.Repeat("+", add))
	buf.WriteString(strings.Repeat("-", del))
	buf.WriteByte('\n')
}

func writeStatSummary(buf *bytes.Buffer, rows []statRow) {
	added, deleted := 0, 0
	for _, row := range rows {
		if row.binary {
			continue
		}
		added += row.added
		deleted += row.deleted
	}
	fmt.Fprintf(buf, " %d %s changed", len(rows), plural(len(rows), "file", "files"))
	if added > 0 || deleted == 0 {
		fmt.Fprintf(buf, ", %d %s(+)", added, plural(added, "insertion", "insertions"))
	}
	if deleted > 0 || added == 0 {
		fmt.Fprintf(buf, ", %d %s(-)", deleted, plural(deleted, "deletion", "deletions"))
	}
	buf.WriteByte('\n')
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}
