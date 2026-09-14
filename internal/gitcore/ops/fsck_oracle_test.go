//go:build oracle

package ops

import (
	"bytes"
	"compress/zlib"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func gitUnreachable(out string) []string {
	var ids []string
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "unreachable" {
			ids = append(ids, fields[2])
		}
	}
	slices.Sort(ids)
	return ids
}

func TestOracleFsckAgreesWithGitAboutAHealthyRepository(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	r := o.openRepo(dir)

	report, err := Fsck(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}

	if !report.Healthy() {
		t.Fatalf("problems in a repository git calls healthy: %+v", report.Problems)
	}
	var ours []string
	for _, id := range report.Unreachable {
		ours = append(ours, id.String())
	}
	theirs := gitUnreachable(o.run(dir, "fsck", "--unreachable", "--no-progress"))
	if !slices.Equal(ours, theirs) {
		t.Fatalf("unreachable objects differ:\nours   %v\ntheirs %v", ours, theirs)
	}
}

func TestOracleFsckFindsTheDamageGitFinds(t *testing.T) {
	o := newOracle(t)
	dir, _ := buildOracleMaintenanceRepo(o)
	missing := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD:dir/b.txt"))
	corrupt := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD:a.txt"))
	r := o.openRepo(dir)
	for _, id := range []string{missing, corrupt} {
		path := filepath.Join(r.ObjectsDir(), id[:2], id[2:])
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if id == missing {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		_, _ = writer.Write(object.EncodeLooseRaw(object.TypeBlob, []byte("not what the name promises\n")))
		_ = writer.Close()
		if err := os.WriteFile(path, compressed.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Fsck(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}

	gitOut, gitErr := o.attempt(dir, "fsck", "--no-progress", "--no-dangling")
	if gitErr == nil {
		t.Fatalf("git calls the damaged repository healthy: %s", gitOut)
	}
	missingID, _ := hash.Parse(missing)
	corruptID, _ := hash.Parse(corrupt)
	if !problemOf(report, FsckMissing, missingID) || !problemOf(report, FsckCorrupt, corruptID) {
		t.Fatalf("report = %+v", report.Problems)
	}
	for _, id := range []string{missing, corrupt} {
		if !strings.Contains(gitErr.Error()+gitOut, id) {
			t.Fatalf("git does not mention %s: %v %s", id, gitErr, gitOut)
		}
	}
}
