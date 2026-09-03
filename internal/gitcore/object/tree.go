package object

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Mode uint32

const (
	ModeTree       Mode = 0o040000
	ModeBlob       Mode = 0o100644
	ModeExecutable Mode = 0o100755
	ModeSymlink    Mode = 0o120000
	ModeSubmodule  Mode = 0o160000
)

const modeTypeMask Mode = 0o170000

type TreeEntry struct {
	Mode Mode
	Name string
	ID   hash.ObjectID
}

type Tree struct {
	Entries []TreeEntry
}

func (m Mode) String() string {
	return strconv.FormatUint(uint64(m), 8)
}

func (m Mode) IsTree() bool {
	return m&modeTypeMask == ModeTree
}

func (m Mode) IsRegular() bool {
	return m&modeTypeMask == 0o100000
}

func (m Mode) IsSymlink() bool {
	return m&modeTypeMask == ModeSymlink
}

func (m Mode) IsSubmodule() bool {
	return m&modeTypeMask == ModeSubmodule
}

func (m Mode) ObjectType() Type {
	switch {
	case m.IsTree():
		return TypeTree
	case m.IsSubmodule():
		return TypeCommit
	default:
		return TypeBlob
	}
}

func ParseMode(text string) (Mode, error) {
	value, err := strconv.ParseUint(text, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidMode, err)
	}
	mode := Mode(value)
	switch mode & modeTypeMask {
	case ModeTree, 0o100000, ModeSymlink, ModeSubmodule:
		return mode, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrInvalidMode, text)
	}
}

func ParseTreeSeq(data []byte) iter.Seq2[TreeEntry, error] {
	return func(yield func(TreeEntry, error) bool) {
		for len(data) > 0 {
			modeText, afterMode, found := bytes.Cut(data, []byte(" "))
			if !found {
				yield(TreeEntry{}, fmt.Errorf("%w: tree entry without a space after the mode", ErrMalformed))
				return
			}
			mode, err := ParseMode(string(modeText))
			if err != nil {
				yield(TreeEntry{}, err)
				return
			}
			name, afterName, found := bytes.Cut(afterMode, []byte{0})
			if !found {
				yield(TreeEntry{}, fmt.Errorf("%w: tree entry without a name terminator", ErrMalformed))
				return
			}
			if len(name) == 0 {
				yield(TreeEntry{}, fmt.Errorf("%w: tree entry with an empty name", ErrMalformed))
				return
			}
			if len(afterName) < hash.Size {
				yield(TreeEntry{}, fmt.Errorf("%w: tree entry %q without a full object id", ErrMalformed, name))
				return
			}
			entry := TreeEntry{Mode: mode, Name: string(name)}
			copy(entry.ID[:], afterName[:hash.Size])
			data = afterName[hash.Size:]
			if !yield(entry, nil) {
				return
			}
		}
	}
}

func ParseTree(data []byte) (*Tree, error) {
	tree := new(Tree{})
	for entry, err := range ParseTreeSeq(data) {
		if err != nil {
			return nil, err
		}
		tree.Entries = append(tree.Entries, entry)
	}
	return tree, nil
}

func (t *Tree) Type() Type {
	return TypeTree
}

func (t *Tree) Encode() []byte {
	var buf bytes.Buffer
	for _, entry := range t.Entries {
		buf.WriteString(entry.Mode.String())
		buf.WriteByte(' ')
		buf.WriteString(entry.Name)
		buf.WriteByte(0)
		buf.Write(entry.ID[:])
	}
	return buf.Bytes()
}

func (t *Tree) WriteTo(w io.Writer) (int64, error) {
	return writeAll(w, t.Encode())
}

func (t *Tree) ID() hash.ObjectID {
	return identify(t)
}

func (t *Tree) Sort() {
	slices.SortStableFunc(t.Entries, CompareEntries)
}

func (t *Tree) IsSorted() bool {
	return slices.IsSortedFunc(t.Entries, CompareEntries)
}

func (t *Tree) Find(name string) (TreeEntry, bool) {
	index := slices.IndexFunc(t.Entries, func(entry TreeEntry) bool { return entry.Name == name })
	if index < 0 {
		return TreeEntry{}, false
	}
	return t.Entries[index], true
}

func CompareEntries(a, b TreeEntry) int {
	return strings.Compare(sortKey(a), sortKey(b))
}

func sortKey(entry TreeEntry) string {
	if entry.Mode.IsTree() {
		return entry.Name + "/"
	}
	return entry.Name
}
