//go:build oracle

package transport

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestGitDaemonProtocolV0FetchSendsOnlyWhatTheNegotiationLeftOut(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	basePath := t.TempDir()
	work := filepath.Join(basePath, "work")
	bare := filepath.Join(basePath, "repo.git")
	oracleGit(t, basePath, "init", "-q", "-b", "main", work)
	var commits []hash.ObjectID
	for i := range 70 {
		if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte(strconv.Itoa(i)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		oracleGit(t, work, "add", "f.txt")
		oracleGit(t, work, "commit", "-q", "-m", "c"+strconv.Itoa(i))
		id, err := hash.Parse(strings.TrimSpace(string(oracleGit(t, work, "rev-parse", "HEAD"))))
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, id)
	}
	oracleGit(t, basePath, "init", "-q", "-b", "main", "--bare", bare)
	oracleGit(t, work, "push", "-q", bare, "main:main")

	port, ok := freeTCPPort(t)
	if !ok {
		t.Skip("could not find a free tcp port")
	}
	addr, ok := startGitDaemon(t, basePath, port)
	if !ok {
		t.Skip("could not start git daemon")
	}

	haves := manyHaves(40, 100)
	common := commits[49]
	for i := 49; i >= 0; i-- {
		haves = append(haves, commits[i])
	}
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	neg := &sliceNegotiator{haves: haves}
	tip := commits[len(commits)-1]
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{tip}}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	packData, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading the pack returned error %v", err)
	}
	if len(packData) < 12 || !bytes.Equal(packData[:4], []byte("PACK")) {
		t.Fatalf("the response is not a pack: %q", packData[:min(len(packData), 16)])
	}
	if !slices.Contains(neg.common, common) {
		t.Fatalf("the server did not acknowledge %s as common: %v", common, neg.common)
	}
	listed := strings.Fields(string(oracleGit(t, bare, "rev-list", "--objects", tip.String(), "--not", common.String())))
	objects := 0
	for _, field := range listed {
		if _, err := hash.Parse(field); err == nil {
			objects++
		}
	}
	if got := binary.BigEndian.Uint32(packData[8:12]); int(got) != objects {
		t.Fatalf("the pack holds %d objects, git rev-list names %d beyond the common commit", got, objects)
	}
}
