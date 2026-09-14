//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
)

func (b *mergeBuilder) gitlink(rel, id string) {
	b.o.t.Helper()
	b.git("rm", "-q", "-r", "--cached", "--ignore-unmatch", "--", rel)
	if err := os.RemoveAll(filepath.Join(b.dir, filepath.FromSlash(rel))); err != nil {
		b.o.t.Fatal(err)
	}
	b.git("update-index", "--add", "--cacheinfo", "160000,"+id+","+rel)
}

func (b *mergeBuilder) fileOverGitlink(rel, text string) {
	b.o.t.Helper()
	b.git("rm", "-q", "-r", "--cached", "--ignore-unmatch", "--", rel)
	if err := os.RemoveAll(filepath.Join(b.dir, filepath.FromSlash(rel))); err != nil {
		b.o.t.Fatal(err)
	}
	b.write(map[string]string{rel: text})
}

func gitlinkHistory(base, ours, theirs func(b *mergeBuilder)) func(b *mergeBuilder) {
	return func(b *mergeBuilder) {
		b.write(map[string]string{"keep": "keep\n"})
		base(b)
		b.git("commit", "-q", "--allow-empty", "-m", "base")
		b.git("branch", "feature")
		ours(b)
		b.git("commit", "-q", "--allow-empty", "-m", "ours")
		b.git("checkout", "-q", "feature")
		theirs(b)
		b.git("commit", "-q", "--allow-empty", "-m", "theirs")
		b.git("checkout", "-q", "main")
	}
}

func noChange(*mergeBuilder) {}

func pointTo(rel, id string) func(b *mergeBuilder) {
	return func(b *mergeBuilder) { b.gitlink(rel, id) }
}

func fileAt(rel, text string) func(b *mergeBuilder) {
	return func(b *mergeBuilder) { b.fileOverGitlink(rel, text) }
}

func readsConflictLikeTheIndex(paths ...string) func(b *mergeBuilder, ours bool) {
	return func(b *mergeBuilder, ours bool) {
		b.o.t.Helper()
		if !ours {
			return
		}
		r := b.o.openRepo(b.dir)
		for _, path := range paths {
			want := map[string]string{}
			for line := range strings.Lines(b.o.run(b.dir, "ls-files", "-s", "--", path)) {
				fields := strings.Fields(line)
				text := "Subproject commit " + fields[1] + "\n"
				if fields[0] != "160000" {
					text = b.o.run(b.dir, "cat-file", "blob", fields[1])
				}
				want[fields[2]] = text
			}
			file, err := ReadConflict(b.o.t.Context(), r, path)
			if err != nil {
				b.o.t.Fatalf("ReadConflict(%s): %v", path, err)
			}
			got := map[string]string{}
			for stage, side := range map[string]struct {
				data []byte
				has  bool
			}{"1": {file.Base, file.HasBase}, "2": {file.Ours, file.HasOurs}, "3": {file.Theirs, file.HasTheirs}} {
				if side.has {
					got[stage] = string(side.data)
				}
			}
			if len(got) != len(want) {
				b.o.t.Fatalf("%s: ReadConflict sides %q, index stages %q", path, got, want)
			}
			for stage, text := range want {
				if got[stage] != text {
					b.o.t.Fatalf("%s stage %s: ReadConflict %q, index %q", path, stage, got[stage], text)
				}
			}
		}
	}
}

func gitlinkMergeScenarios() []mergeScenario {
	return []mergeScenario{
		{name: "submodule moved apart", target: "feature", after: readsConflictLikeTheIndex("sub"),
			setup: gitlinkHistory(pointTo("sub", firstGitlinkID), pointTo("sub", secondGitlinkID), pointTo("sub", "3333333333333333333333333333333333333333"))},
		{name: "submodule moved against its deletion", target: "feature", after: readsConflictLikeTheIndex("sub"),
			setup: gitlinkHistory(pointTo("sub", firstGitlinkID), pointTo("sub", secondGitlinkID), func(b *mergeBuilder) { b.git("rm", "-q", "--cached", "sub") })},
		{name: "submodule moved on one side only", target: "feature",
			setup: gitlinkHistory(pointTo("sub", firstGitlinkID), noChange, pointTo("sub", secondGitlinkID))},
		{name: "file against a submodule", target: "feature", after: readsConflictLikeTheIndex("p", "p~HEAD"),
			setup: gitlinkHistory(noChange, fileAt("p", "file\n"), pointTo("p", firstGitlinkID))},
		{name: "submodule against a file", target: "feature", after: readsConflictLikeTheIndex("p", "p~feature"),
			setup: gitlinkHistory(noChange, pointTo("p", firstGitlinkID), fileAt("p", "file\n"))},
		{name: "edited file against a submodule", target: "feature",
			setup: gitlinkHistory(fileAt("q", "base\n"), fileAt("q", "edit\n"), pointTo("q", firstGitlinkID))},
		{name: "moved submodule against a file", target: "feature",
			setup: gitlinkHistory(pointTo("g", firstGitlinkID), pointTo("g", secondGitlinkID), fileAt("g", "file\n"))},
	}
}
