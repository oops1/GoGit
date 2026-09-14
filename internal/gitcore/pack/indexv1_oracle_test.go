//go:build oracle

package pack

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOracleVersionOneIndexWrittenByGitServesEveryObject(t *testing.T) {
	oracle := newGitOracle(t)
	name := oracle.packName()
	dir := t.TempDir()
	packPath := writeTemp(t, filepath.Join(dir, name+packSuffix), readFixture(t, filepath.Join(oracle.packDir(), name+packSuffix)))
	indexPath := filepath.Join(dir, name+indexSuffix)
	oracle.run(oracle.repo, "index-pack", "--index-version=1", "-o", indexPath, packPath)

	index, err := OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	defer func() { _ = index.Close() }()
	if index.Version() != indexVersionOne {
		t.Fatalf("git wrote an index of version %d, want 1", index.Version())
	}
	show := oracle.command(oracle.repo, "show-index")
	show.Stdin = bytes.NewReader(readFixture(t, indexPath))
	listed, err := show.Output()
	if err != nil {
		t.Fatalf("git show-index returned error %v", err)
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(listed), "\r\n", "\n")), "\n")
	if len(lines) != index.Count() {
		t.Fatalf("git show-index listed %d entries, the index holds %d", len(lines), index.Count())
	}
	for position, line := range lines {
		fields := strings.Fields(line)
		entry, err := index.EntryAt(position)
		if err != nil {
			t.Fatalf("EntryAt(%d) returned error %v", position, err)
		}
		if len(fields) != 2 || fields[0] != strconv.FormatInt(entry.Offset, 10) || fields[1] != entry.ID.String() {
			t.Fatalf("entry %d = %s at %d, git shows %q", position, entry.ID, entry.Offset, line)
		}
	}

	store := openStore(t, dir)
	names := oracle.objectNames()
	for id, want := range oracle.catFile(names) {
		kind, data, ok, err := store.Get(id)
		if !ok || err != nil || kind != want.kind || !bytes.Equal(data, want.data) {
			t.Fatalf("Get(%s) through the version 1 index returned (%v, %v)", id, ok, err)
		}
	}
}
