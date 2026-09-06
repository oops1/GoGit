package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const shallowLockSuffix = ".lock"

func (r *Repository) WriteShallow(ids []hash.ObjectID) error {
	if len(ids) == 0 {
		return removeShallow(r.commonRoot, shallowFileName)
	}
	return writeShallow(r.commonRoot, shallowFileName, ids)
}

func (r *Repository) IsShallow() (bool, error) {
	shallow, err := r.Shallow()
	if err != nil {
		return false, err
	}
	return len(shallow) > 0, nil
}

func encodeShallow(ids []hash.ObjectID) []byte {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id.String())
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func writeShallow(root *os.Root, rel string, ids []hash.ObjectID) error {
	lock := rel + shallowLockSuffix
	fh, err := root.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidShallowFile, lock, err)
	}
	_, err = fh.Write(encodeShallow(ids))
	err = errors.Join(err, fh.Close())
	if err == nil {
		err = root.Rename(lock, rel)
	}
	if err != nil {
		err = errors.Join(err, root.Remove(lock))
	}
	return err
}

func removeShallow(root *os.Root, rel string) error {
	err := root.Remove(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
