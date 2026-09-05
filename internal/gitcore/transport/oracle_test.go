//go:build oracle

package transport

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func oracleGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=oracle",
		"GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle",
		"GIT_COMMITTER_EMAIL=oracle@example.com",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s returned error %v", strings.Join(args, " "), err)
	}
	return out
}

func TestParseAdvertisementMatchesGitUploadPack(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir := t.TempDir()
	oracleGit(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	oracleGit(t, dir, "add", "a.txt")
	oracleGit(t, dir, "commit", "-q", "-m", "initial")
	oracleGit(t, dir, "tag", "-a", "v1", "-m", "v1")
	oracleGit(t, dir, "branch", "other")

	advertised := oracleGit(t, dir, "upload-pack", "--advertise-refs", dir)
	adv, err := ParseAdvertisement(bytes.NewReader(advertised))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}

	wantRefs := map[string]string{}
	forEachRef := oracleGit(t, dir, "for-each-ref", "--format=%(objectname) %(refname)")
	for _, line := range strings.Split(strings.TrimSpace(string(forEachRef)), "\n") {
		if line == "" {
			continue
		}
		oid, name, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("unexpected for-each-ref line %q", line)
		}
		wantRefs[name] = oid
	}

	gotRefs := map[string]string{}
	for _, ref := range adv.Refs {
		if ref.Name == "HEAD" {
			continue
		}
		gotRefs[ref.Name] = ref.ID.String()
	}
	if len(gotRefs) != len(wantRefs) {
		t.Fatalf("ParseAdvertisement found %d refs %v, git for-each-ref reports %d %v",
			len(gotRefs), gotRefs, len(wantRefs), wantRefs)
	}
	for name, oid := range wantRefs {
		if gotRefs[name] != oid {
			t.Errorf("ref %s: ParseAdvertisement reports %s, git reports %s", name, gotRefs[name], oid)
		}
	}

	symbolicRef := strings.TrimSpace(string(oracleGit(t, dir, "symbolic-ref", "HEAD")))
	if adv.Head != symbolicRef {
		t.Errorf("Advertisement.Head = %q, git symbolic-ref HEAD reports %q", adv.Head, symbolicRef)
	}

	tagRef, ok := findRef(adv.Refs, "refs/tags/v1")
	if !ok {
		t.Fatalf("ParseAdvertisement did not advertise refs/tags/v1")
	}
	peeled := strings.TrimSpace(string(oracleGit(t, dir, "rev-parse", "v1^{}")))
	if tagRef.Peeled.String() != peeled {
		t.Errorf("refs/tags/v1 Peeled = %q, git rev-parse v1^{} reports %q", tagRef.Peeled, peeled)
	}
}

func findRef(refs []Ref, name string) (Ref, bool) {
	for _, ref := range refs {
		if ref.Name == name {
			return ref, true
		}
	}
	return Ref{}, false
}

func TestParseAdvertisementMatchesGitUploadPackEmptyRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir := t.TempDir()
	oracleGit(t, dir, "init", "-q", "-b", "main")

	advertised := oracleGit(t, dir, "upload-pack", "--advertise-refs", dir)
	adv, err := ParseAdvertisement(bytes.NewReader(advertised))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v for an empty repository", err)
	}
	if len(adv.Refs) != 0 {
		t.Fatalf("ParseAdvertisement reported refs %v for an empty repository", adv.Refs)
	}
}
