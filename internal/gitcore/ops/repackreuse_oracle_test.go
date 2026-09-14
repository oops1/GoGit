//go:build oracle

package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func buildOracleSimilarHistory(o *oracle, commits int) string {
	dir := o.repoDir("similar")
	newOracleRepo(o, dir)
	var text strings.Builder
	for line := range 300 {
		fmt.Fprintf(&text, "line %03d of a steadily growing file\n", line)
	}
	for step := range commits {
		fmt.Fprintf(&text, "change %d\n", step)
		o.write(dir, fmt.Sprintf("dir%d/file.txt", step%3), text.String())
		o.run(dir, "add", ".")
		o.run(dir, "commit", "-q", "-m", fmt.Sprintf("change %d", step))
	}
	return dir
}

type verifiedObject struct {
	size  int64
	delta bool
}

func verifyPackObjects(o *oracle, dir, idx string) []verifiedObject {
	var objects []verifiedObject
	for line := range strings.Lines(o.run(dir, "verify-pack", "-v", idx)) {
		fields := strings.Fields(line)
		if len(fields) < 5 || len(fields[0]) != 40 {
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			o.t.Fatalf("verify-pack line %q: %v", line, err)
		}
		objects = append(objects, verifiedObject{size: size, delta: len(fields) == 7})
	}
	return objects
}

func deltasOver(objects []verifiedObject, size int64) int {
	count := 0
	for _, obj := range objects {
		if obj.delta && obj.size > size {
			count++
		}
	}
	return count
}

func onlyIndexIn(o *oracle, packDir string) string {
	matches, err := filepath.Glob(filepath.Join(packDir, "*.idx"))
	if err != nil || len(matches) != 1 {
		o.t.Fatalf("pack indexes = %v, %v", matches, err)
	}
	return matches[0]
}

func TestOracleRepackReusesTheDeltasOfAGitPackAndStaysValid(t *testing.T) {
	o := newOracle(t)
	dir := buildOracleSimilarHistory(o, 30)
	o.run(dir, "repack", "-a", "-d", "-f", "-q", "--window=10", "--depth=50")
	r := o.openRepo(dir)
	gitIndex := onlyIndexIn(o, r.PackDir())
	gitObjects := verifyPackObjects(o, dir, gitIndex)
	gitInfo, err := os.Stat(strings.TrimSuffix(gitIndex, ".idx") + ".pack")
	if err != nil {
		t.Fatal(err)
	}

	first, err := Repack(t.Context(), r, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	index := filepath.Join(r.PackDir(), first.Pack+".idx")
	ours := verifyPackObjects(o, dir, index)
	o.run(dir, "index-pack", "-o", filepath.Join(t.TempDir(), "check.idx"), filepath.Join(r.PackDir(), first.Pack+".pack"))
	o.run(dir, "fsck", "--no-dangling", "--no-progress")
	if len(ours) != len(gitObjects) || deltasOver(ours, 0) < deltasOver(gitObjects, 0) {
		t.Fatalf("ours holds %d objects and %d deltas, git's %d objects and %d deltas", len(ours), deltasOver(ours, 0), len(gitObjects), deltasOver(gitObjects, 0))
	}
	if first.Bytes > gitInfo.Size()+gitInfo.Size()/10 {
		t.Fatalf("ours takes %d bytes, git's pack %d", first.Bytes, gitInfo.Size())
	}
	t.Logf("git pack %d bytes with %d deltas, ours %d bytes with %d deltas", gitInfo.Size(), deltasOver(gitObjects, 0), first.Bytes, deltasOver(ours, 0))

	second, err := Repack(t.Context(), r, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Pack != first.Pack {
		t.Fatalf("repacking again wrote %s instead of %s", second.Pack, first.Pack)
	}
}

func TestOracleRepackLeavesFilesAboveTheBigFileThresholdWholeLikeGit(t *testing.T) {
	o := newOracle(t)
	dir := buildOracleSimilarHistory(o, 6)
	o.run(dir, "config", "core.bigFileThreshold", "1k")
	r := o.openRepo(dir)

	result, err := Repack(t.Context(), r, RepackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ours := verifyPackObjects(o, dir, filepath.Join(r.PackDir(), result.Pack+".idx"))
	o.run(dir, "repack", "-a", "-d", "-f", "-q")
	theirs := verifyPackObjects(o, dir, onlyIndexIn(o, r.PackDir()))
	if deltasOver(ours, 1024) != 0 || deltasOver(theirs, 1024) != 0 || len(ours) != len(theirs) {
		t.Fatalf("big deltas: ours %d, git %d; objects: ours %d, git %d", deltasOver(ours, 1024), deltasOver(theirs, 1024), len(ours), len(theirs))
	}
}
