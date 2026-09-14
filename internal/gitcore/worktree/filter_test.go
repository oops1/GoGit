package worktree

import "testing"

func TestStatusMarksPathsBehindFiltersItCannotRun(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[filter \"indent\"]\n\tclean = indent\n\tsmudge = indent\n[filter \"lfs\"]\n\tprocess = git-lfs filter-process\n")
	tr.repo = tr.reopen()
	tr.stage(".gitattributes", "*.c filter=indent\n*.bin filter=lfs -text\n")
	tr.stage("a.c", "int a;\n")
	tr.stageContent("b.bin", lfsPointerOf("binary"))
	tr.stage("c.txt", "plain\n")
	tr.stage("same.c", "int same;\n")
	tr.commit("filters")
	tr.writeFile("a.c", "int  a;\n")
	tr.writeFile("b.bin", "changed")
	tr.writeFile("c.txt", "changed\n")
	tr.writeFile("new.c", "int b;\n")
	tr.writeFile("dir/x.c", "int x;\n")

	status, err := tr.openWith(Options{IncludeUnmodified: true}).Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	want := map[string]bool{"a.c": true, "new.c": true, "same.c": true, "b.bin": false, "c.txt": false, "dir/": false, ".gitattributes": false}
	entries := entryMap(status.Entries)
	for rel, unsupported := range want {
		entry, ok := entries[rel]
		if !ok || entry.FilterUnsupported != unsupported {
			t.Errorf("%s = %+v (%v), want FilterUnsupported %v", rel, entry, ok, unsupported)
		}
	}
}
