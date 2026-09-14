package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type LFSPointerReason uint8

const (
	LFSObjectMissing LFSPointerReason = iota + 1
	LFSSmudgeSkipped
)

type LFSPointerFile struct {
	Path   string
	OID    string
	Size   int64
	Reason LFSPointerReason
}

type CheckoutReport struct {
	LFSPointers []LFSPointerFile
	Unfiltered  []string
}

type lfsObjectFile interface {
	io.ReadCloser
	Stat() (fs.FileInfo, error)
}

var (
	lfsOpenObject = func(name string) (lfsObjectFile, error) { return os.Open(name) }
	lfsObjectID   = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
)

func (r *CheckoutReport) addUnfiltered(path string) {
	if r != nil {
		r.Unfiltered = append(r.Unfiltered, path)
	}
}

func (r *CheckoutReport) addPointer(path string, pointer attributes.LFSPointer, reason LFSPointerReason) {
	if r != nil {
		r.LFSPointers = append(r.LFSPointers, LFSPointerFile{Path: path, OID: pointer.OID, Size: pointer.Size, Reason: reason})
	}
}

func (r *CheckoutReport) sort() {
	if r == nil {
		return
	}
	slices.SortFunc(r.LFSPointers, func(a, b LFSPointerFile) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(r.Unfiltered)
}

func lfsObjectsDir(r *repo.Repository) string {
	storage, _ := r.Config().Get("lfs.storage")
	if storage == "" {
		storage = "lfs"
	}
	if !filepath.IsAbs(storage) {
		storage = filepath.Join(r.CommonDir(), storage)
	}
	return filepath.Join(storage, "objects")
}

func (w *workingTree) writeCheckedOut(rel string, mode object.Mode, blob []byte, report *CheckoutReport) error {
	if mode.IsSymlink() {
		return writeWorktreeBlob(w, rel, mode, blob)
	}
	policy := w.attrs.Policy(rel)
	data := policy.ToWorkingTree(blob)
	switch policy.Smudge {
	case attributes.FilterLFS:
		return w.smudgeLFS(rel, mode, data, policy.SkipSmudge, report)
	case attributes.FilterExternal:
		report.addUnfiltered(rel)
	}
	return writeWorktreeBlob(w, rel, mode, data)
}

func (w *workingTree) smudgeLFS(rel string, mode object.Mode, data []byte, skip bool, report *CheckoutReport) error {
	pointer, ok := attributes.DecodeLFSPointer(data)
	switch {
	case !ok:
		return writeWorktreeBlob(w, rel, mode, data)
	case pointer.Extended:
		report.addUnfiltered(rel)
		return writeWorktreeBlob(w, rel, mode, data)
	case pointer.Size == 0:
		return writeWorktreeBlob(w, rel, mode, nil)
	case skip || !w.lfsFetch.Allows(rel):
		report.addPointer(rel, pointer, LFSSmudgeSkipped)
		return writeWorktreeBlob(w, rel, mode, pointer.Encode())
	}
	copied, err := w.copyLFSObject(rel, mode, pointer)
	if err != nil || copied {
		return err
	}
	report.addPointer(rel, pointer, LFSObjectMissing)
	return writeWorktreeBlob(w, rel, mode, pointer.Encode())
}

func (w *workingTree) copyLFSObject(rel string, mode object.Mode, pointer attributes.LFSPointer) (bool, error) {
	if !lfsObjectID.MatchString(pointer.OID) {
		return false, nil
	}
	file, err := lfsOpenObject(filepath.Join(w.lfsObjects, pointer.OID[:2], pointer.OID[2:4], pointer.OID))
	if err != nil {
		return false, nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != pointer.Size {
		return false, nil
	}
	digest := sha256.New()
	err = writeWorktreeStream(w, rel, mode, func(out io.Writer) error {
		_, err := io.Copy(io.MultiWriter(out, digest), file)
		return err
	})
	if err != nil {
		return false, err
	}
	return hex.EncodeToString(digest.Sum(nil)) == pointer.OID, nil
}
