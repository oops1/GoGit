package diff

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const devNull = "/dev/null"

func Unified(w io.Writer, file File, opts Options) error {
	var buf bytes.Buffer
	writeUnified(&buf, file, opts.normalized())
	_, err := w.Write(buf.Bytes())
	return err
}

func (f File) paths() (oldPath, newPath string) {
	oldPath, newPath = f.OldPath, f.NewPath
	if oldPath == "" {
		oldPath = newPath
	}
	if newPath == "" {
		newPath = oldPath
	}
	return oldPath, newPath
}

func (f File) labels() (oldLabel, newLabel string) {
	oldPath, newPath := f.paths()
	oldLabel, newLabel = quoteTwo("a/", oldPath), quoteTwo("b/", newPath)
	if f.Status == StatusAdded {
		oldLabel = devNull
	}
	if f.Status == StatusDeleted {
		newLabel = devNull
	}
	return oldLabel, newLabel
}

func (f File) mustShowHeader() bool {
	return f.Status != StatusModified || f.OldMode != f.NewMode
}

func (f File) hasContent() bool {
	return len(f.Hunks) > 0 || (f.Binary && f.OldID != f.NewID)
}

func labelSeparator(label string) string {
	if strings.ContainsRune(label, ' ') {
		return "\t"
	}
	return ""
}

func writeUnified(buf *bytes.Buffer, file File, opts Options) {
	if len(file.Parts) > 0 {
		for _, part := range file.Parts {
			writeUnified(buf, part, opts)
		}
		return
	}
	if !file.mustShowHeader() && !file.hasContent() {
		return
	}
	oldPath, newPath := file.paths()
	fmt.Fprintf(buf, "diff --git %s %s\n", quoteTwo("a/", oldPath), quoteTwo("b/", newPath))

	switch file.Status {
	case StatusAdded:
		fmt.Fprintf(buf, "new file mode %06o\n", uint32(file.NewMode))
	case StatusDeleted:
		fmt.Fprintf(buf, "deleted file mode %06o\n", uint32(file.OldMode))
	default:
		if file.OldMode != file.NewMode {
			fmt.Fprintf(buf, "old mode %06o\n", uint32(file.OldMode))
			fmt.Fprintf(buf, "new mode %06o\n", uint32(file.NewMode))
		}
	}

	switch file.Status {
	case StatusRenamed:
		fmt.Fprintf(buf, "similarity index %d%%\n", file.Similarity)
		fmt.Fprintf(buf, "rename from %s\n", quoteCStyle(oldPath))
		fmt.Fprintf(buf, "rename to %s\n", quoteCStyle(newPath))
	case StatusCopied:
		fmt.Fprintf(buf, "similarity index %d%%\n", file.Similarity)
		fmt.Fprintf(buf, "copy from %s\n", quoteCStyle(oldPath))
		fmt.Fprintf(buf, "copy to %s\n", quoteCStyle(newPath))
	}

	if file.OldID != file.NewID {
		fmt.Fprintf(buf, "index %s..%s", file.OldID.String()[:opts.Abbrev], file.NewID.String()[:opts.Abbrev])
		if file.OldMode == file.NewMode {
			fmt.Fprintf(buf, " %06o", uint32(file.OldMode))
		}
		buf.WriteByte('\n')
	}

	oldLabel, newLabel := file.labels()
	if file.Binary {
		if file.OldID != file.NewID {
			fmt.Fprintf(buf, "Binary files %s and %s differ\n", oldLabel, newLabel)
		}
		return
	}
	if len(file.Hunks) == 0 {
		return
	}
	fmt.Fprintf(buf, "--- %s%s\n", oldLabel, labelSeparator(oldLabel))
	fmt.Fprintf(buf, "+++ %s%s\n", newLabel, labelSeparator(newLabel))
	for _, hunk := range file.Hunks {
		writeHunk(buf, hunk)
	}
}

func writeHunk(buf *bytes.Buffer, hunk Hunk) {
	buf.WriteString("@@ -")
	writeRange(buf, hunk.OldStart, hunk.OldLines)
	buf.WriteString(" +")
	writeRange(buf, hunk.NewStart, hunk.NewLines)
	buf.WriteString(" @@")
	if hunk.Header != "" {
		buf.WriteByte(' ')
		buf.WriteString(hunk.Header)
	}
	buf.WriteByte('\n')
	for _, line := range hunk.Lines {
		buf.WriteString(line.Kind.prefix())
		buf.WriteString(line.Text)
		buf.WriteByte('\n')
		if line.NoNewline {
			buf.WriteString("\\ No newline at end of file\n")
		}
	}
}

func writeRange(buf *bytes.Buffer, start, count int) {
	if count == 0 {
		start--
	}
	buf.WriteString(strconv.Itoa(start))
	if count != 1 {
		buf.WriteByte(',')
		buf.WriteString(strconv.Itoa(count))
	}
}
