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

type writerOracleObject struct {
	kind object.Type
	data []byte
}

func readWriterOracleCatFileBatch(t testing.TB, repo string, env []string, ids []hash.ObjectID) map[hash.ObjectID]writerOracleObject {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "cat-file", "--batch")
	cmd.Dir = repo
	cmd.Env = env
	var stdin strings.Builder
	for _, id := range ids {
		stdin.WriteString(id.String())
		stdin.WriteByte('\n')
	}
	cmd.Stdin = strings.NewReader(stdin.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git cat-file --batch returned error %v", err)
	}
	found := make(map[hash.ObjectID]writerOracleObject, len(ids))
	reader := bufio.NewReader(bytes.NewReader(out))
	for range ids {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the cat-file header returned error %v", err)
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("cat-file header %q holds %d fields, want 3", line, len(fields))
		}
		kind, err := object.ParseType(fields[1])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[1], err)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("Atoi(%q) returned error %v", fields[2], err)
		}
		body := make([]byte, size+1)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatalf("reading %d bytes of %s returned error %v", size, fields[0], err)
		}
		id, err := hash.Parse(fields[0])
		if err != nil {
			t.Fatalf("hash.Parse(%q) returned error %v", fields[0], err)
		}
		found[id] = writerOracleObject{kind: kind, data: body[:size]}
	}
	return found
}

func writerOracleEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + home,
		"USERPROFILE=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
}

func runWriterOracleGit(t testing.TB, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s returned error %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestOracleWritePackAndWriteIndexVerifyWithSystemGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	env := writerOracleEnv(home)
	repo := filepath.Join(root, "repo")
	runWriterOracleGit(t, root, env, "init", "-q", "-b", "main", repo)

	source := newFakeSource()
	var ids []hash.ObjectID
	for i := range 14 {
		content := append(similarBlob(i%9, 500), []byte(fmt.Sprintf("#%d\n", i))...)
		ids = append(ids, source.add(object.TypeBlob, content))
	}
	ids = append(ids, source.add(object.TypeTree, []byte("tree content for the oracle test, not a real tree encoding\n")))
	ids = append(ids, source.add(object.TypeCommit, []byte("commit content for the oracle test, not a real commit encoding\n")))

	var packBuf bytes.Buffer
	result, err := WritePack(t.Context(), &packBuf, source, ids, WriteOptions{Window: 10, Depth: 8})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	var idxBuf bytes.Buffer
	if err := WriteIndex(&idxBuf, result.Entries, result.Checksum); err != nil {
		t.Fatalf("WriteIndex returned error %v", err)
	}

	packDir := filepath.Join(repo, ".git", "objects", "pack")
	name := "pack-" + result.Checksum.String()
	packPath := filepath.Join(packDir, name+packSuffix)
	idxPath := filepath.Join(packDir, name+indexSuffix)
	if err := os.WriteFile(packPath, packBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", packPath, err)
	}
	if err := os.WriteFile(idxPath, idxBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", idxPath, err)
	}

	runWriterOracleGit(t, repo, env, "index-pack", "--verify", packPath)
	verifyOut := runWriterOracleGit(t, repo, env, "verify-pack", "-v", idxPath)
	if strings.Count(verifyOut, "\n") < len(ids) {
		t.Fatalf("verify-pack reported fewer lines than the %d written objects:\n%s", len(ids), verifyOut)
	}

	found := readWriterOracleCatFileBatch(t, repo, env, ids)
	for _, id := range ids {
		wantKind, wantData, err := source.Get(id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", id, err)
		}
		got, ok := found[id]
		if !ok {
			t.Fatalf("git cat-file --batch did not report %s", id)
		}
		if got.kind != wantKind {
			t.Errorf("cat-file --batch %s kind = %s, want %s", id, got.kind, wantKind)
		}
		if !bytes.Equal(got.data, wantData) {
			t.Errorf("cat-file --batch %s gave %d bytes, want %d", id, len(got.data), len(wantData))
		}
	}

	ourIndex, err := OpenIndex(idxPath)
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	defer func() { _ = ourIndex.Close() }()
	if err := ourIndex.Verify(); err != nil {
		t.Fatalf("Index.Verify returned error %v", err)
	}
	ourPack, err := OpenPack(packPath, WithIndex(ourIndex))
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	defer func() { _ = ourPack.Close() }()
	if err := ourPack.Verify(); err != nil {
		t.Fatalf("Pack.Verify returned error %v", err)
	}
	for _, entry := range result.Entries {
		wantKind, wantData, _ := source.Get(entry.ID)
		kind, data, err := ourPack.ObjectAt(entry.Offset)
		if err != nil {
			t.Fatalf("ObjectAt(%d) returned error %v", entry.Offset, err)
		}
		if kind != wantKind || !bytes.Equal(data, wantData) {
			t.Errorf("ObjectAt(%d) did not reproduce %s", entry.Offset, entry.ID)
		}
	}
}

func TestOracleWritePackThinPackIndexPacksCleanly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	env := writerOracleEnv(home)
	repo := filepath.Join(root, "repo")
	runWriterOracleGit(t, root, env, "init", "-q", "-b", "main", repo)

	source := newFakeSource()
	base := similarBlob(0, 400)
	baseID := source.add(object.TypeBlob, base)
	target := append(bytes.Clone(base), []byte("thin pack tail unique to the target object\n")...)
	targetID := source.add(object.TypeBlob, target)

	var thinBuf bytes.Buffer
	result, err := WritePack(t.Context(), &thinBuf, source, []hash.ObjectID{targetID},
		WriteOptions{Window: 4, Depth: 4, Thin: map[hash.ObjectID]struct{}{baseID: {}}})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(result.Entries))
	}
	pending := filepath.Join(root, "thin.pack")
	if err := os.WriteFile(pending, thinBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	writeLooseBlobForOracle(t, repo, baseID, base)

	cmd := exec.CommandContext(t.Context(), "git", "index-pack", "--stdin", "--fix-thin")
	cmd.Dir = repo
	cmd.Env = env
	stdin, err := os.Open(pending)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = stdin.Close() }()
	cmd.Stdin = stdin
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git index-pack --stdin --fix-thin returned error %v: %s", err, out)
	}

	got := runWriterOracleGit(t, repo, env, "cat-file", "-p", targetID.String())
	if !bytes.Equal([]byte(got), target) {
		t.Errorf("cat-file -p %s gave %d bytes, want %d", targetID, len(got), len(target))
	}
}

func writeLooseBlobForOracle(t testing.TB, repo string, id hash.ObjectID, data []byte) string {
	t.Helper()
	writer, err := object.WriteLooseRaw(mustOpenRoot(t, filepath.Join(repo, ".git", "objects")), object.TypeBlob, data)
	if err != nil {
		t.Fatalf("WriteLooseRaw returned error %v", err)
	}
	if writer != id {
		t.Fatalf("WriteLooseRaw wrote %s, want %s", writer, id)
	}
	return filepath.Join(repo, ".git", "objects", id.String()[:2], id.String()[2:])
}

func mustOpenRoot(t testing.TB, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot(%q) returned error %v", dir, err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}
