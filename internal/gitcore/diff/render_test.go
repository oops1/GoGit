package diff

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func blobFile(oldPath, newPath string, oldData, newData []byte, opts Options) (File, bool) {
	if bytes.Equal(oldData, newData) {
		return File{}, false
	}
	file := File{
		OldPath: oldPath,
		NewPath: newPath,
		OldMode: object.ModeBlob,
		NewMode: object.ModeBlob,
		OldID:   hash.SumSHA1("blob", oldData),
		NewID:   hash.SumSHA1("blob", newData),
		Status:  StatusModified,
		OldSize: len(oldData),
		NewSize: len(newData),
	}
	if isBinary(oldData) || isBinary(newData) {
		file.Binary = true
		return file, true
	}
	file.Hunks = Blobs(oldData, newData, opts)
	return file, true
}

func renderFiles(t *testing.T, files []File, kind variantKind, opts Options) string {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch kind {
	case variantStat:
		err = Stat(&buf, files, opts)
	case variantNumStat:
		err = NumStat(&buf, files)
	case variantPatch:
		for _, file := range files {
			if err = Unified(&buf, file, opts); err != nil {
				break
			}
		}
	}
	if err != nil {
		t.Fatalf("rendering returned error %v", err)
	}
	return buf.String()
}

func renderPair(t *testing.T, pair corpusPair, v variant) string {
	t.Helper()
	file, changed := blobFile(pairPath("old", pair.name), pairPath("new", pair.name), []byte(pair.old), []byte(pair.new), v.opts)
	if !changed {
		return ""
	}
	return renderFiles(t, []File{file}, v.kind, v.opts)
}

func pairPath(side, name string) string {
	return side + "/" + name + ".txt"
}
