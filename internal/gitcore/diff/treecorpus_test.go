package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type entrySpec struct {
	mode object.Mode
	data string
}

func blobSpec(data string) entrySpec {
	return entrySpec{mode: object.ModeBlob, data: data}
}

func execSpec(data string) entrySpec {
	return entrySpec{mode: object.ModeExecutable, data: data}
}

func linkSpec(target string) entrySpec {
	return entrySpec{mode: object.ModeSymlink, data: target}
}

func moduleSpec(id string) entrySpec {
	return entrySpec{mode: object.ModeSubmodule, data: id}
}

type treeFiles map[string]entrySpec

type treePair struct {
	name string
	old  treeFiles
	new  treeFiles
}

type objectWriter interface {
	writeBlob(data []byte) hash.ObjectID
	writeTree(entries []object.TreeEntry) hash.ObjectID
}

type memoryStore struct {
	kinds map[hash.ObjectID]object.Type
	data  map[hash.ObjectID][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{kinds: make(map[hash.ObjectID]object.Type), data: make(map[hash.ObjectID][]byte)}
}

func (m *memoryStore) Get(id hash.ObjectID) (object.Type, []byte, error) {
	kind, ok := m.kinds[id]
	if !ok {
		return 0, nil, fmt.Errorf("memory store: %w: %s", ErrMissingBlob, id)
	}
	return kind, m.data[id], nil
}

func (m *memoryStore) put(kind object.Type, data []byte) hash.ObjectID {
	id := hash.SumSHA1(kind.String(), data)
	m.kinds[id] = kind
	m.data[id] = data
	return id
}

func (m *memoryStore) writeBlob(data []byte) hash.ObjectID {
	return m.put(object.TypeBlob, data)
}

func (m *memoryStore) writeTree(entries []object.TreeEntry) hash.ObjectID {
	tree := &object.Tree{Entries: entries}
	return m.put(object.TypeTree, tree.Encode())
}

func entryID(w objectWriter, spec entrySpec) hash.ObjectID {
	if spec.mode.IsSubmodule() {
		id, err := hash.Parse(spec.data)
		if err != nil {
			panic(err)
		}
		return id
	}
	return w.writeBlob([]byte(spec.data))
}

func buildTree(w objectWriter, files treeFiles) hash.ObjectID {
	subdirs := make(map[string]treeFiles)
	var entries []object.TreeEntry
	for _, path := range slices.Sorted(maps(files)) {
		spec := files[path]
		dir, rest, nested := strings.Cut(path, "/")
		if nested {
			if subdirs[dir] == nil {
				subdirs[dir] = treeFiles{}
			}
			subdirs[dir][rest] = spec
			continue
		}
		entries = append(entries, object.TreeEntry{Mode: spec.mode, Name: path, ID: entryID(w, spec)})
	}
	for _, dir := range slices.Sorted(maps(subdirs)) {
		entries = append(entries, object.TreeEntry{Mode: object.ModeTree, Name: dir, ID: buildTree(w, subdirs[dir])})
	}
	slices.SortStableFunc(entries, object.CompareEntries)
	return w.writeTree(entries)
}

func maps[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range m {
			if !yield(key) {
				return
			}
		}
	}
}

func storeTreePair(t *testing.T, pair treePair) (*memoryStore, hash.ObjectID, hash.ObjectID) {
	t.Helper()
	store := newMemoryStore()
	return store, buildTree(store, pair.old), buildTree(store, pair.new)
}

const (
	moduleA = "1111111111111111111111111111111111111111"
	moduleB = "2222222222222222222222222222222222222222"
)

func poem(seed string) string {
	var out strings.Builder
	for at := range 40 {
		fmt.Fprintf(&out, "%s line %d\n", seed, at)
	}
	return out.String()
}

func binaryBlob(seed byte) string {
	raw := make([]byte, 64)
	for at := range raw {
		raw[at] = byte(at) ^ seed
	}
	return string(raw)
}

func treeCorpus() []treePair {
	base := treeFiles{"keep.txt": blobSpec("keep\n"), "story.txt": blobSpec(poem("story"))}
	withExtra := func(extra treeFiles) treeFiles {
		out := treeFiles{}
		for name, spec := range base {
			out[name] = spec
		}
		for name, spec := range extra {
			out[name] = spec
		}
		return out
	}
	return []treePair{
		{"add-file", base, withExtra(treeFiles{"added.txt": blobSpec("added\n")})},
		{"add-empty-file", base, withExtra(treeFiles{"empty.txt": blobSpec("")})},
		{"delete-file", withExtra(treeFiles{"gone.txt": blobSpec("gone\n")}), base},
		{
			"modify-file",
			base,
			withExtra(treeFiles{"story.txt": blobSpec(strings.Replace(poem("story"), "story line 7\n", "story line seven\n", 1))}),
		},
		{"mode-change", base, withExtra(treeFiles{"keep.txt": execSpec("keep\n")})},
		{
			"mode-and-content",
			base,
			withExtra(treeFiles{"keep.txt": execSpec("keep\nmore\n")}),
		},
		{
			"exact-rename",
			withExtra(treeFiles{"before.txt": blobSpec(poem("moved"))}),
			withExtra(treeFiles{"after.txt": blobSpec(poem("moved"))}),
		},
		{
			"rename-with-edits",
			withExtra(treeFiles{"before.txt": blobSpec(poem("moved"))}),
			withExtra(treeFiles{"after.txt": blobSpec(strings.Replace(poem("moved"), "moved line 3\n", "moved line three\n", 1))}),
		},
		{
			"rename-into-subdir",
			withExtra(treeFiles{"top.txt": blobSpec(poem("deep"))}),
			withExtra(treeFiles{"sub/top.txt": blobSpec(poem("deep"))}),
		},
		{
			"rename-out-of-subdir",
			withExtra(treeFiles{"sub/inner.txt": blobSpec(poem("inner"))}),
			withExtra(treeFiles{"inner.txt": blobSpec(poem("inner"))}),
		},
		{
			"rename-and-modify-other",
			withExtra(treeFiles{"before.txt": blobSpec(poem("moved")), "other.txt": blobSpec("one\ntwo\n")}),
			withExtra(treeFiles{"after.txt": blobSpec(poem("moved")), "other.txt": blobSpec("one\nTWO\n")}),
		},
		{
			"copy-exact",
			withExtra(treeFiles{"source.txt": blobSpec(poem("copied"))}),
			withExtra(treeFiles{"source.txt": blobSpec(poem("copied")), "clone.txt": blobSpec(poem("copied"))}),
		},
		{
			"copy-with-edits",
			withExtra(treeFiles{"source.txt": blobSpec(poem("copied"))}),
			withExtra(treeFiles{
				"source.txt": blobSpec(poem("copied")),
				"clone.txt":  blobSpec(strings.Replace(poem("copied"), "copied line 9\n", "copied line nine\n", 1)),
			}),
		},
		{
			"copy-from-modified-source",
			withExtra(treeFiles{"source.txt": blobSpec(poem("copied"))}),
			withExtra(treeFiles{
				"source.txt": blobSpec(poem("copied") + "tail\n"),
				"clone.txt":  blobSpec(poem("copied")),
			}),
		},
		{
			"long-paths",
			withExtra(treeFiles{
				"very/deeply/nested/directory/structure/with/many/segments/report.txt": blobSpec(poem("report")),
				"aVeryLongSingleSegmentFileNameWithoutAnySlashesAtAllInIt.txt":         blobSpec(poem("single")),
			}),
			withExtra(treeFiles{
				"very/deeply/nested/directory/structure/with/many/segments/report.txt": blobSpec(poem("edited")),
				"aVeryLongSingleSegmentFileNameWithoutAnySlashesAtAllInIt.txt":         blobSpec(poem("other")),
			}),
		},
		{
			"wide-change-counts",
			withExtra(treeFiles{"small.txt": blobSpec("a\nb\nc\n"), "huge.txt": blobSpec(poem("huge"))}),
			withExtra(treeFiles{
				"small.txt": blobSpec("a\nB\nc\n"),
				"huge.txt":  blobSpec(poem("huge") + poem("more") + poem("even more")),
			}),
		},
		{
			"two-identical-renames",
			withExtra(treeFiles{"a.txt": blobSpec(poem("twin")), "b.txt": blobSpec(poem("twin"))}),
			withExtra(treeFiles{"c.txt": blobSpec(poem("twin")), "d.txt": blobSpec(poem("twin"))}),
		},
		{
			"rename-below-threshold",
			withExtra(treeFiles{"before.txt": blobSpec(poem("moved"))}),
			withExtra(treeFiles{"after.txt": blobSpec("nothing in common at all\n")}),
		},
		{
			"basename-rename",
			withExtra(treeFiles{"one/report.txt": blobSpec(poem("report"))}),
			withExtra(treeFiles{
				"two/report.txt": blobSpec(strings.Replace(poem("report"), "report line 1\n", "report line one\n", 1)),
			}),
		},
		{
			"binary-add",
			base,
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(0))}),
		},
		{
			"binary-modify",
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(0))}),
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(3))}),
		},
		{
			"binary-delete",
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(0))}),
			base,
		},
		{
			"binary-mode-change",
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(0))}),
			withExtra(treeFiles{"blob.bin": entrySpec{mode: object.ModeExecutable, data: binaryBlob(0)}}),
		},
		{
			"binary-rename",
			withExtra(treeFiles{"blob.bin": blobSpec(binaryBlob(0))}),
			withExtra(treeFiles{"moved.bin": blobSpec(binaryBlob(0))}),
		},
		{
			"symlink-add",
			base,
			withExtra(treeFiles{"link": linkSpec("keep.txt")}),
		},
		{
			"symlink-change",
			withExtra(treeFiles{"link": linkSpec("keep.txt")}),
			withExtra(treeFiles{"link": linkSpec("story.txt")}),
		},
		{
			"symlink-to-blob",
			withExtra(treeFiles{"link": linkSpec("keep.txt")}),
			withExtra(treeFiles{"link": blobSpec("keep.txt")}),
		},
		{
			"blob-to-symlink",
			withExtra(treeFiles{"thing": blobSpec("story.txt")}),
			withExtra(treeFiles{"thing": linkSpec("story.txt")}),
		},
		{
			"submodule-add",
			base,
			withExtra(treeFiles{"module": moduleSpec(moduleA)}),
		},
		{
			"submodule-change",
			withExtra(treeFiles{"module": moduleSpec(moduleA)}),
			withExtra(treeFiles{"module": moduleSpec(moduleB)}),
		},
		{
			"submodule-delete",
			withExtra(treeFiles{"module": moduleSpec(moduleA)}),
			base,
		},
		{
			"submodule-to-blob",
			withExtra(treeFiles{"module": moduleSpec(moduleA)}),
			withExtra(treeFiles{"module": blobSpec("plain\n")}),
		},
		{
			"file-to-dir",
			withExtra(treeFiles{"thing": blobSpec("plain\n")}),
			withExtra(treeFiles{"thing/inner.txt": blobSpec("plain\n")}),
		},
		{
			"dir-to-file",
			withExtra(treeFiles{"thing/inner.txt": blobSpec("plain\n")}),
			withExtra(treeFiles{"thing": blobSpec("plain\n")}),
		},
		{
			"deep-subdir-change",
			withExtra(treeFiles{"a/b/c/deep.txt": blobSpec(poem("deep"))}),
			withExtra(treeFiles{"a/b/c/deep.txt": blobSpec(strings.Replace(poem("deep"), "deep line 5\n", "deep line five\n", 1))}),
		},
		{
			"many-files",
			treeFiles{
				"one.txt": blobSpec("one\n"), "two.txt": blobSpec("two\n"),
				"three.txt": blobSpec("three\n"), "dir/four.txt": blobSpec("four\n"),
			},
			treeFiles{
				"one.txt": blobSpec("ONE\n"), "two.txt": blobSpec("two\n"),
				"dir/four.txt": blobSpec("FOUR\n"), "dir/five.txt": blobSpec("five\n"),
			},
		},
		{
			"quoted-path",
			base,
			withExtra(treeFiles{"stra\\nge \"name\".txt": blobSpec("odd\n")}),
		},
		{"empty-to-full", treeFiles{}, base},
		{"full-to-empty", base, treeFiles{}},
	}
}

type treeVariant struct {
	name string
	args []string
	kind variantKind
	opts Options
}

func treeVariants() []treeVariant {
	return []treeVariant{
		{
			name: "renames-copies",
			args: []string{"-p", "-M", "-C"},
			opts: withOptions(func(o *Options) { o.DetectCopies = true }),
		},
		{name: "renames", args: []string{"-p", "-M"}, opts: Defaults()},
		{
			name: "no-renames",
			args: []string{"-p", "--no-renames"},
			opts: withOptions(func(o *Options) { o.DetectRenames = false }),
		},
		{
			name: "stat",
			args: []string{"--stat", "-M", "-C"},
			kind: variantStat,
			opts: withOptions(func(o *Options) { o.DetectCopies = true }),
		},
		{
			name: "numstat",
			args: []string{"--numstat", "-M", "-C"},
			kind: variantNumStat,
			opts: withOptions(func(o *Options) { o.DetectCopies = true }),
		},
	}
}
