//go:build oracle

package pack

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type gitOracle struct {
	t    *testing.T
	root string
	repo string
	env  []string
}

type catFileObject struct {
	kind object.Type
	data []byte
}

func newGitOracle(t *testing.T) *gitOracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	oracle := &gitOracle{
		t:    t,
		root: root,
		repo: filepath.Join(root, "repo"),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
			"GIT_AUTHOR_DATE=2024-01-02T03:04:05 +0000",
			"GIT_COMMITTER_DATE=2024-01-02T03:04:05 +0000",
		},
	}
	oracle.build()
	return oracle
}

func (o *gitOracle) command(dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	return cmd
}

func (o *gitOracle) raw(dir string, args ...string) []byte {
	o.t.Helper()
	out, err := o.command(dir, args...).Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v", strings.Join(args, " "), err)
	}
	return out
}

func (o *gitOracle) run(dir string, args ...string) string {
	o.t.Helper()
	return string(o.raw(dir, args...))
}

func (o *gitOracle) lines(dir string, args ...string) []string {
	o.t.Helper()
	text := strings.ReplaceAll(o.run(dir, args...), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func (o *gitOracle) build() {
	o.t.Helper()
	o.run(o.root, "init", "-q", "-b", "main", o.repo)
	for _, args := range [][]string{
		{"config", "user.name", "oracle"},
		{"config", "user.email", "oracle@example.com"},
		{"config", "core.autocrlf", "false"},
	} {
		o.run(o.repo, args...)
	}
	if err := os.MkdirAll(filepath.Join(o.repo, "lib", "deep"), 0o755); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	for revision := range 12 {
		o.writeRevision(revision + 1)
		o.run(o.repo, "add", "-A")
		o.run(o.repo, "commit", "-q", "-m", fmt.Sprintf("revision %d", revision+1))
	}
	o.run(o.repo, "tag", "-a", "v1.0", "-m", "annotated oracle tag", "HEAD~3")
	o.run(o.repo, "tag", "lightweight", "HEAD~1")
	o.run(o.repo, "branch", "side", "HEAD~5")
	o.run(o.repo, "repack", "-a", "-d", "-f", "--depth=50", "--window=3")
}

func (o *gitOracle) writeRevision(revision int) {
	o.t.Helper()
	var body strings.Builder
	for line := range 600 {
		state := 0
		if line%8+1 <= revision {
			state = 1
		}
		fmt.Fprintf(&body, "line %05d state %d padding aaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbb\n", line, state)
	}
	o.write("big.txt", body.String())
	o.write(filepath.Join("lib", "lib.go"), fmt.Sprintf("package lib\n\nconst Revision = %d\n", revision))
	o.write(filepath.Join("lib", "deep", "inner.txt"), fmt.Sprintf("nested %d\n", revision))
}

func (o *gitOracle) write(name, content string) {
	o.t.Helper()
	if err := os.WriteFile(filepath.Join(o.repo, name), []byte(content), 0o600); err != nil {
		o.t.Fatalf("WriteFile(%q) returned error %v", name, err)
	}
}

func (o *gitOracle) packDir() string {
	return filepath.Join(o.repo, ".git", "objects", "pack")
}

func (o *gitOracle) packName() string {
	o.t.Helper()
	matches, err := filepath.Glob(filepath.Join(o.packDir(), "*"+packSuffix))
	if err != nil {
		o.t.Fatalf("Glob returned error %v", err)
	}
	if len(matches) != 1 {
		o.t.Fatalf("the repository holds %d packfiles, want 1", len(matches))
	}
	return strings.TrimSuffix(filepath.Base(matches[0]), packSuffix)
}

func (o *gitOracle) objectNames() []string {
	o.t.Helper()
	var names []string
	for _, line := range o.lines(o.repo, "rev-list", "--objects", "--all") {
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	if len(names) == 0 {
		o.t.Fatal("git listed no objects")
	}
	return names
}

func (o *gitOracle) catFile(names []string) map[hash.ObjectID]catFileObject {
	o.t.Helper()
	cmd := o.command(o.repo, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git cat-file --batch returned error %v", err)
	}
	found := make(map[hash.ObjectID]catFileObject, len(names))
	reader := bufio.NewReader(bytes.NewReader(out))
	for range names {
		head, err := reader.ReadString('\n')
		if err != nil {
			o.t.Fatalf("reading the cat-file header returned error %v", err)
		}
		fields := strings.Fields(head)
		if len(fields) != 3 {
			o.t.Fatalf("the cat-file header %q holds %d fields, want 3", head, len(fields))
		}
		kind, err := object.ParseType(fields[1])
		if err != nil {
			o.t.Fatalf("ParseType(%q) returned error %v", fields[1], err)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			o.t.Fatalf("Atoi(%q) returned error %v", fields[2], err)
		}
		data := make([]byte, size+1)
		if _, err := io.ReadFull(reader, data); err != nil {
			o.t.Fatalf("reading %d bytes of %s returned error %v", size, fields[0], err)
		}
		found[parseID(o.t, fields[0])] = catFileObject{kind: kind, data: data[:size]}
	}
	return found
}

func TestOracleStoreServesEveryObjectByteForByte(t *testing.T) {
	oracle := newGitOracle(t)
	names := oracle.objectNames()
	wanted := oracle.catFile(names)
	store, err := Open(oracle.packDir())
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	for id, want := range wanted {
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != want.kind {
			t.Errorf("Get(%s) gave %s, git says %s", id, kind, want.kind)
		}
		if !bytes.Equal(data, want.data) {
			t.Errorf("Get(%s) gave %d bytes, git gave %d", id, len(data), len(want.data))
		}
		if rebuilt := hash.SumSHA1(kind.String(), data); rebuilt != id {
			t.Errorf("Get(%s) rebuilt %s", id, rebuilt)
		}
	}
	if got := len(wanted); store.Count() != got {
		t.Fatalf("Count = %d, git listed %d objects", store.Count(), got)
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestOracleVerifyPackAgreesOnEveryEntry(t *testing.T) {
	oracle := newGitOracle(t)
	name := oracle.packName()
	records := parseVerifyLines(t, oracle.lines(oracle.repo, "verify-pack", "-v",
		filepath.Join(oracle.packDir(), name+indexSuffix)))
	if len(records) == 0 {
		t.Fatal("git verify-pack listed no objects")
	}
	index, err := OpenIndex(filepath.Join(oracle.packDir(), name+indexSuffix))
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	defer func() { _ = index.Close() }()
	packfile, err := OpenPack(filepath.Join(oracle.packDir(), name+packSuffix), WithIndex(index))
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	defer func() { _ = packfile.Close() }()
	if err := index.Verify(); err != nil {
		t.Fatalf("Index.Verify returned error %v", err)
	}
	if err := packfile.Verify(); err != nil {
		t.Fatalf("Pack.Verify returned error %v", err)
	}
	if index.PackHash() != packfile.Checksum() {
		t.Fatalf("the index names %s, the packfile trailer is %s", index.PackHash(), packfile.Checksum())
	}
	deltas := 0
	for _, record := range records {
		position, ok, err := index.Position(record.id)
		if err != nil || !ok {
			t.Fatalf("Position(%s) returned (%v, %v)", record.id, ok, err)
		}
		entry, err := index.EntryAt(position)
		if err != nil {
			t.Fatalf("EntryAt(%d) returned error %v", position, err)
		}
		if entry.Offset != record.offset {
			t.Errorf("EntryAt(%d) gave offset %d, git says %d", position, entry.Offset, record.offset)
		}
		head, err := packfile.HeaderAt(record.offset)
		if err != nil {
			t.Fatalf("HeaderAt(%d) returned error %v", record.offset, err)
		}
		if head.Size != record.size {
			t.Errorf("HeaderAt(%d) declares %d bytes, git says %d", record.offset, head.Size, record.size)
		}
		if head.Kind.IsDelta() != record.isDelta {
			t.Errorf("HeaderAt(%d) gave %s, git says delta=%v", record.offset, head.Kind, record.isDelta)
		}
		if !record.isDelta {
			if head.Kind.Type() != record.kind {
				t.Errorf("HeaderAt(%d) gave %s, git says %s", record.offset, head.Kind, record.kind)
			}
			continue
		}
		deltas++
		base, ok := index.Find(record.base)
		if !ok {
			t.Fatalf("the index does not hold the base %s", record.base)
		}
		if head.Kind == KindOffsetDelta && head.BaseOffset != base {
			t.Errorf("HeaderAt(%d) points at %d, git says the base sits at %d", record.offset, head.BaseOffset, base)
		}
		if head.Kind == KindRefDelta && head.BaseID != record.base {
			t.Errorf("HeaderAt(%d) points at %s, git says %s", record.offset, head.BaseID, record.base)
		}
		kind, data, err := packfile.ObjectAt(record.offset)
		if err != nil {
			t.Fatalf("ObjectAt(%d) returned error %v", record.offset, err)
		}
		if rebuilt := hash.SumSHA1(kind.String(), data); rebuilt != record.id {
			t.Errorf("ObjectAt(%d) rebuilt %s, want %s", record.offset, rebuilt, record.id)
		}
	}
	if deltas == 0 {
		t.Fatal("git packed no deltas")
	}
}

func TestOracleShowIndexAgreesOnChecksumsAndOffsets(t *testing.T) {
	oracle := newGitOracle(t)
	name := oracle.packName()
	cmd := oracle.command(oracle.repo, "show-index")
	source, err := os.Open(filepath.Join(oracle.packDir(), name+indexSuffix))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = source.Close() }()
	cmd.Stdin = source
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show-index returned error %v", err)
	}
	index, err := OpenIndex(filepath.Join(oracle.packDir(), name+indexSuffix))
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	defer func() { _ = index.Close() }()
	packfile, err := OpenPack(filepath.Join(oracle.packDir(), name+packSuffix))
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	defer func() { _ = packfile.Close() }()
	raw := readFixture(t, filepath.Join(oracle.packDir(), name+packSuffix))
	reader, err := NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewReader returned error %v", err)
	}
	sums := make(map[int64]uint32)
	for {
		entry, err := reader.NextObject()
		if err != nil {
			break
		}
		sums[entry.Header.Offset] = entry.CRC32
	}
	seen := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		id := parseID(t, fields[1])
		offset := parseInt(t, fields[0])
		want, err := strconv.ParseUint(strings.Trim(fields[2], "()"), 16, 32)
		if err != nil {
			t.Fatalf("ParseUint(%q) returned error %v", fields[2], err)
		}
		position, ok, err := index.Position(id)
		if err != nil || !ok {
			t.Fatalf("Position(%s) returned (%v, %v)", id, ok, err)
		}
		entry, err := index.EntryAt(position)
		if err != nil {
			t.Fatalf("EntryAt(%d) returned error %v", position, err)
		}
		if entry.Offset != offset || entry.CRC32 != uint32(want) {
			t.Errorf("EntryAt(%d) = (%d, %08x), git says (%d, %08x)",
				position, entry.Offset, entry.CRC32, offset, want)
		}
		if got := sums[offset]; got != uint32(want) {
			t.Errorf("the reader computed %08x for the object at %d, git says %08x", got, offset, want)
		}
		seen++
	}
	if seen != index.Count() {
		t.Fatalf("git show-index listed %d objects, the index holds %d", seen, index.Count())
	}
}
