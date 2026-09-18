package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var errSubmoduleFault = errors.New("submodule fault")

func failRefsUnder(t *testing.T, part string) {
	t.Helper()
	replaceSeam(t, &refsOpen, func(opts refs.Options) (*refs.Store, error) {
		if strings.Contains(filepath.ToSlash(opts.GitDir), part) {
			return nil, errSubmoduleFault
		}
		return refs.Open(opts)
	})
}

func failObjectsUnder(t *testing.T, part string) {
	t.Helper()
	replaceSeam(t, &odbOpen, func(dir string, opts odb.Options) (*odb.DB, error) {
		if strings.Contains(filepath.ToSlash(dir), part) {
			return nil, errSubmoduleFault
		}
		return odb.Open(dir, opts)
	})
}

func failConfigWriteAt(t *testing.T, at int) {
	t.Helper()
	calls := 0
	replaceSeam(t, &writeSubmoduleConfig, func(path string, values ...[2]string) error {
		calls++
		if calls == at {
			return errSubmoduleFault
		}
		return setConfigValues(path, values...)
	})
}

func updatedLibProject(t *testing.T) *libProject {
	t.Helper()
	p := newLibProject(t, libGitmodules)
	p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
	return p
}

func TestSubmoduleInitReportsConfigWriteFailures(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	failConfigWriteAt(t, 1)
	if err := SubmoduleInit(t.Context(), p.super.reopen(), nil, SubmoduleInitOptions{}); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("active write returned %v", err)
	}

	p = newLibProject(t, libGitmodules)
	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	failConfigWriteAt(t, 1)
	if err := SubmoduleInit(t.Context(), p.super.reopen(), nil, SubmoduleInitOptions{}); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("url write returned %v", err)
	}
}

func TestSubmoduleInitReportsABrokenActiveFlagAmongActiveModules(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	p.super.appendConfig("[submodule]\n\tactive = .\n[submodule \"lib\"]\n\tactive = maybe\n")
	if err := SubmoduleInit(t.Context(), p.super.reopen(), nil, SubmoduleInitOptions{}); err == nil {
		t.Fatal("SubmoduleInit accepted active = maybe")
	}
}

func TestSuperprojectURLResolution(t *testing.T) {
	fresh := newTestRepo(t)
	fresh.commitFile("a.txt", "a\n", "a")
	s := &superproject{repo: fresh.repo, cfg: fresh.repo.Config()}
	if url, err := s.resolveURL("./lib", ""); err != nil || url != filepath.ToSlash(fresh.dir)+"/lib" {
		t.Fatalf("a superproject without a remote resolved %q, %v", url, err)
	}

	failRefsUnder(t, "")
	if _, err := s.resolveURL("../lib", ""); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("a refs failure returned %v", err)
	}
	if url := s.gitmodulesURL(submodule.Module{URL: "../lib"}); url != "" {
		t.Fatalf("gitmodulesURL = %q", url)
	}
	if url := s.gitmodulesURLOr("../lib"); url != "../lib" {
		t.Fatalf("gitmodulesURLOr = %q", url)
	}
	if _, err := s.remoteBranch(updateRecord{link: gitlinkEntry{module: submodule.Module{Branch: "."}}}); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("remoteBranch returned %v", err)
	}
	if _, err := resolveRef(fresh.repo, refs.HEAD); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("resolveRef returned %v", err)
	}
}

func TestReachabilityTreatsFailuresAsUnreachable(t *testing.T) {
	p := updatedLibProject(t)
	sub := p.submoduleRepo(t)
	db := sub.db()
	sig := object.Signature{Name: "ann", Email: "ann@example.com"}
	commit, _ := db.Commit(p.one)
	dangling, err := db.PutObject(&object.Commit{Tree: commit.Tree, Author: sig, Committer: sig, Message: "dangling\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !tipReachable(t.Context(), sub.repo, p.one) || tipReachable(t.Context(), sub.repo, dangling) {
		t.Fatal("reachability of a referenced and a dangling commit is wrong")
	}
	if hasObject(sub.repo, hash.SumSHA1("commit", []byte("absent"))) {
		t.Fatal("an absent object was found")
	}

	sub.writeRawRef("refs/heads/broken", "garbage\n")
	if tipReachable(t.Context(), sub.repo, p.one) {
		t.Fatal("an unreadable ref still counted as a tip")
	}

	failRefsUnder(t, "modules")
	if tipReachable(t.Context(), sub.repo, p.one) {
		t.Fatal("a refs failure still counted as reachable")
	}
	failObjectsUnder(t, "modules")
	if tipReachable(t.Context(), sub.repo, p.one) || hasObject(sub.repo, p.one) {
		t.Fatal("an object database failure still found the commit")
	}
}

func TestSubmoduleSyncReportsSeamFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, p *libProject)
	}{
		{"path", func(t *testing.T, _ *libProject) {
			replaceSeam(t, &validateSubmodulePath, func(string, string) error { return errSubmoduleFault })
		}},
		{"remote", func(t *testing.T, p *libProject) {
			p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../moved\n")
			failRefsUnder(t, "modules")
		}},
		{"reopen", func(t *testing.T, _ *libProject) {
			replaceSeam(t, &reopenSubmodule, func(*repo.Repository) (*repo.Repository, error) { return nil, errSubmoduleFault })
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := updatedLibProject(t)
			tc.setup(t, p)
			if _, err := p.sync(t, nil, true); !errors.Is(err, errSubmoduleFault) {
				t.Fatalf("SubmoduleSync returned %v", err)
			}
		})
	}
}

func TestSubmoduleSyncSkipsAGitlinkMissingFromGitmodules(t *testing.T) {
	p := updatedLibProject(t)
	p.super.setGitlink("unknown", p.one)
	if events, err := p.sync(t, nil, false); err != nil || len(events) != 1 {
		t.Fatalf("SubmoduleSync = %+v, %v", events, err)
	}
}

func TestSubmoduleUpdateReportsSeamFailures(t *testing.T) {
	tests := []struct {
		name    string
		fresh   bool
		opts    SubmoduleUpdateOptions
		setup   func(t *testing.T, p *libProject)
		wantErr error
	}{
		{"reopen after init", true, SubmoduleUpdateOptions{Init: true}, func(t *testing.T, _ *libProject) {
			replaceSeam(t, &reopenSubmodule, func(*repo.Repository) (*repo.Repository, error) { return nil, errSubmoduleFault })
		}, errSubmoduleFault},
		{"clone path", true, SubmoduleUpdateOptions{Init: true}, func(t *testing.T, _ *libProject) {
			replaceSeam(t, &validateSubmodulePath, func(string, string) error { return errSubmoduleFault })
		}, errSubmoduleFault},
		{"update path", false, SubmoduleUpdateOptions{}, func(t *testing.T, _ *libProject) {
			replaceSeam(t, &validateSubmodulePath, func(string, string) error { return errSubmoduleFault })
		}, errSubmoduleFault},
		{"core.worktree write", false, SubmoduleUpdateOptions{}, func(t *testing.T, _ *libProject) { failConfigWriteAt(t, 1) }, errSubmoduleFault},
		{"nested reopen", false, SubmoduleUpdateOptions{Recursive: true}, func(t *testing.T, _ *libProject) {
			replaceSeam(t, &reopenSubmodule, func(*repo.Repository) (*repo.Repository, error) { return nil, errSubmoduleFault })
		}, errSubmoduleFault},
		{"nested gitmodules", false, SubmoduleUpdateOptions{Recursive: true}, func(_ *testing.T, p *libProject) {
			p.super.writeFile("libs/lib/.gitmodules", "[submodule \"x\"]\n\tpath\n")
		}, nil},
		{"remote name", false, SubmoduleUpdateOptions{Remote: true}, func(t *testing.T, p *libProject) {
			p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../moved\n")
			failRefsUnder(t, "modules")
		}, errSubmoduleFault},
		{"direct remote name", false, SubmoduleUpdateOptions{}, func(t *testing.T, p *libProject) {
			p.record(p.lib.commitFile("lib.txt", "three\n", "three"))
			p.super.writeFile(".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../moved\n")
			failRefsUnder(t, "modules")
		}, errSubmoduleFault},
		{"work tree directory unreadable", true, SubmoduleUpdateOptions{RequireInit: true}, func(t *testing.T, _ *libProject) {
			replaceSeam(t, &readSubmoduleDir, func(string) ([]os.DirEntry, error) { return nil, errSubmoduleFault })
		}, errSubmoduleFault},
		{"init of an unregistered gitlink", false, SubmoduleUpdateOptions{Init: true}, func(_ *testing.T, p *libProject) {
			p.super.setGitlink("unknown", p.one)
		}, ErrSubmoduleNotRegistered},
		{"broken active flag", false, SubmoduleUpdateOptions{}, func(_ *testing.T, p *libProject) {
			p.super.appendConfig("[submodule \"lib\"]\n\tactive = maybe\n")
		}, nil},
		{"gitfile pointing nowhere", false, SubmoduleUpdateOptions{}, func(_ *testing.T, p *libProject) {
			p.super.writeFile("libs/lib/.git", "gitdir: ../nowhere\n")
		}, ErrSubmoduleNoHead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newLibProject(t, libGitmodules)
			if !tc.fresh {
				p.mustUpdate(t, SubmoduleUpdateOptions{Init: true})
			}
			tc.setup(t, p)
			_, err := p.update(t, nil, tc.opts)
			if err == nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("SubmoduleUpdate returned %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSubmoduleUpdateStopsWhenCancelledBetweenPhases(t *testing.T) {
	p := updatedLibProject(t)
	if err := p.superproject(t).update(newCountingContext(t, 2), false, SubmoduleUpdateOptions{}); err == nil {
		t.Fatal("the update ignored a cancellation before its second phase")
	}
	if _, err := ListSubmodules(newCountingContext(t, 2), p.super.reopen()); err == nil {
		t.Fatal("ListSubmodules ignored a cancellation")
	}
}

func TestSubmoduleUpdateHandlesTheWorkTreeDirectory(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}
	p.mustUpdate(t, SubmoduleUpdateOptions{RequireInit: true})
	if p.libHead(t) != p.one {
		t.Fatal("RequireInit did not clone into a missing directory")
	}

	p = newLibProject(t, libGitmodules)
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}
	p.super.writeFile("libs/lib", "file\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{RequireInit: true}); err == nil {
		t.Fatal("RequireInit accepted a file in place of the directory")
	}

	p = updatedLibProject(t)
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}
	p.super.writeFile("libs/lib", "file\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); err == nil {
		t.Fatal("refilling over a file succeeded")
	}

	p = updatedLibProject(t)
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.super.path(".git/modules/lib/index")); err != nil {
		t.Fatal(err)
	}
	p.super.writeFile(".git/modules/lib/index/keep", "keep\n")
	if _, err := p.update(t, nil, SubmoduleUpdateOptions{}); err == nil {
		t.Fatal("an index that cannot be removed was ignored")
	}
}

func TestSubmoduleUpdateOfAnEmbeddedRepository(t *testing.T) {
	p := newLibProject(t, libGitmodules)
	if err := os.RemoveAll(p.libDir()); err != nil {
		t.Fatal(err)
	}
	embedded := p.world.repoAt("super/libs/lib")
	head := embedded.commitFile("lib.txt", "embedded\n", "embedded")
	p.record(head)
	p.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")

	if events, err := p.update(t, nil, SubmoduleUpdateOptions{}); err != nil || len(events) != 0 {
		t.Fatalf("an embedded repository at its commit = %+v, %v", events, err)
	}

	unborn := newLibProject(t, libGitmodules)
	if err := os.RemoveAll(unborn.libDir()); err != nil {
		t.Fatal(err)
	}
	unborn.world.repoAt("super/libs/lib")
	unborn.super.appendConfig("[submodule \"lib\"]\n\tactive = true\n")
	if _, err := unborn.update(t, nil, SubmoduleUpdateOptions{}); !errors.Is(err, ErrSubmoduleNoHead) {
		t.Fatalf("an unborn embedded repository returned %v", err)
	}
}

func TestListSubmodulesReportsAnObjectDatabaseFailureOfTheSubmodule(t *testing.T) {
	p := updatedLibProject(t)
	p.super.writeFile("libs/lib/.gitmodules", "")
	failObjectsUnder(t, "modules")
	if _, err := ListSubmodules(t.Context(), p.super.reopen()); !errors.Is(err, errSubmoduleFault) {
		t.Fatalf("ListSubmodules returned %v", err)
	}
}
