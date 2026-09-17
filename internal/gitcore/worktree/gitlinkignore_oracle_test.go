//go:build oracle

package worktree

import (
	"testing"
)

func TestOracleSubmoduleIgnoreSettingsMatchGitStatusPorcelainV2(t *testing.T) {
	type state struct {
		name  string
		dirty func(o *oracle, sub string)
	}
	states := []state{
		{"moved, modified and untracked", func(o *oracle, sub string) {
			o.run(sub, "checkout", "-q", "HEAD~1")
			o.write(sub, "a.txt", "changed\n")
			o.write(sub, "loose.txt", "loose\n")
		}},
		{"modified only", func(o *oracle, sub string) {
			o.write(sub, "a.txt", "changed\n")
		}},
		{"untracked only", func(o *oracle, sub string) {
			o.write(sub, "loose.txt", "loose\n")
		}},
		{"moved only", func(o *oracle, sub string) {
			o.run(sub, "checkout", "-q", "HEAD~1")
		}},
	}
	settings := []struct {
		name  string
		apply func(o *oracle, dir, mode string)
	}{
		{"gitmodules", func(o *oracle, dir, mode string) {
			o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = sub\n\turl = ./sub\n\tignore = "+mode+"\n")
		}},
		{"config", func(o *oracle, dir, mode string) {
			o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = sub\n\turl = ./sub\n\tignore = all\n")
			o.run(dir, "config", "submodule.lib.ignore", mode)
		}},
		{"diff.ignoreSubmodules", func(o *oracle, dir, mode string) {
			o.run(dir, "config", "diff.ignoreSubmodules", mode)
		}},
		{"diff.ignoreSubmodules under a module setting", func(o *oracle, dir, mode string) {
			o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = sub\n\turl = ./sub\n\tignore = "+mode+"\n")
			o.run(dir, "config", "diff.ignoreSubmodules", "all")
		}},
	}
	for _, setting := range settings {
		for _, mode := range []string{"none", "untracked", "dirty", "all"} {
			for _, st := range states {
				t.Run(setting.name+"/"+mode+"/"+st.name, func(t *testing.T) {
					o := newOracle(t)
					dir := o.repoDir("work")
					o.run(dir, "init", "-q", "-b", "main", ".")
					o.run(dir, "config", "core.autocrlf", "false")
					sub := o.submoduleSuperproject(dir)
					setting.apply(o, dir, mode)
					st.dirty(o, sub)
					o.compare(dir)
				})
			}
		}
	}
}

func TestOracleIgnoredSubmoduleStillShowsItsStagedCommit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	o.run(dir, "init", "-q", "-b", "main", ".")
	o.run(dir, "config", "core.autocrlf", "false")
	sub := o.submoduleSuperproject(dir)
	o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = sub\n\turl = ./sub\n\tignore = all\n")
	o.run(dir, "add", ".gitmodules")
	o.run(sub, "checkout", "-q", "HEAD~1")
	o.run(dir, "add", "sub")
	o.run(sub, "checkout", "-q", "main")
	o.write(sub, "loose.txt", "loose\n")
	o.compare(dir)
}
