//go:build oracle

package ops

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var gitFsckGitmodulesLine = regexp.MustCompile(`error in (?:blob|tree) ([0-9a-f]+): (gitmodules[A-Za-z]+):`)

func (o *oracle) mktree(dir, listing string, missing bool) string {
	o.t.Helper()
	args := []string{"mktree"}
	if missing {
		args = append(args, "--missing")
	}
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	cmd.Stdin = strings.NewReader(listing)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git mktree %q: %v: %s", listing, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func (o *oracle) literalTree(dir, mode, name, id string) string {
	o.t.Helper()
	raw, err := hex.DecodeString(id)
	if err != nil {
		o.t.Fatal(err)
	}
	path := filepath.Join(o.t.TempDir(), "tree")
	if err := os.WriteFile(path, append([]byte(mode+" "+name+"\x00"), raw...), 0o644); err != nil {
		o.t.Fatal(err)
	}
	return strings.TrimSpace(o.run(dir, "hash-object", "-t", "tree", "--literally", "-w", path))
}

func (o *oracle) blob(dir, text string) string {
	o.t.Helper()
	path := filepath.Join(o.t.TempDir(), "blob")
	o.write(filepath.Dir(path), filepath.Base(path), text)
	return strings.TrimSpace(o.run(dir, "hash-object", "-w", path))
}

func (o *oracle) branchAtTree(dir, name, tree string) {
	o.t.Helper()
	o.run(dir, "update-ref", "refs/heads/"+name, strings.TrimSpace(o.run(dir, "commit-tree", "-m", name, tree)))
}

func TestOracleFsckReportsTheGitmodulesProblemsGitReports(t *testing.T) {
	o := datedOracle(newOracle(t))
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	texts := []string{
		"[submodule \"../../hooks\"]\n\tpath = ok\n\turl = https://example.com/x\n",
		"[submodule \"a\"]\n\turl = -upload-pack=evil\n\tpath = -evil\n\tupdate = !rm -rf\n",
		"[submodule \"b\"]\n\turl = ./%0aevil\n[submodule \"c\"]\n\turl = https://%0ahost/x\n",
		"[submodule \"d\"]\n\turl = ../../:host\n[submodule \"e\"]\n\turl = https:///nohost\n",
		"[submodule \"g\"]\n\turl = git://h/%0a\n[submodule \"h\"]\n\turl = http::https://x%0a/y\n",
		"[submodule \"..\\\\x\"]\n\turl = ssh://ok/%0a\n[submodule \"j\"]\n\turl = ftp://u%0a@h/\n",
		"[submodule \"k\"]\n\turl\n\tpath\n[submodule]\n\turl = -x\n[submodule \"x\"]\nurl = \"-quoted\"\n",
		"[submodule \"../late\"]\n\turl = -z\ngarbage[\n[submodule \"never\"]\n\turl = -never\n",
		"[submodule \"fine\"]\n\turl = https://user:pass@example.com/x\n\tupdate = rebase\n",
	}
	for i, text := range texts {
		tree := o.mktree(dir, "100644 blob "+o.blob(dir, text)+"\t.gitmodules\n", false)
		o.branchAtTree(dir, "case"+strings.Repeat("x", i), tree)
	}
	link := o.blob(dir, "target")
	symlinkTree := o.mktree(dir, "120000 blob "+link+"\t.GitModules\n", false)
	o.branchAtTree(dir, "deep", o.mktree(dir, "040000 tree "+symlinkTree+"\tdeep\n", false))
	short := o.mktree(dir, "100644 blob "+o.blob(dir, "[submodule \"../s\"]\n\turl = -s\n")+"\tGITMOD~1\n", false)
	o.branchAtTree(dir, "short", short)
	o.branchAtTree(dir, "missing", o.mktree(dir, "100644 blob 1111111111111111111111111111111111111111\t.gitmodules\n", true))
	o.branchAtTree(dir, "notblob", o.literalTree(dir, "100644", ".gitmodules", short))
	o.mktree(dir, "100644 blob "+o.blob(dir, "[submodule \"u\"]\n\tpath = -unreachable\n")+"\t.gitmodules\n", false)

	gitOut, gitErr := o.attempt(dir, "fsck", "--no-progress", "--no-dangling")
	if gitErr == nil {
		t.Fatalf("git fsck accepts the repository: %s", gitOut)
	}
	var want []string
	for _, match := range gitFsckGitmodulesLine.FindAllStringSubmatch(gitOut+gitErr.Error(), -1) {
		want = append(want, match[2]+" "+match[1])
	}
	slices.Sort(want)

	report, err := Fsck(t.Context(), o.openRepo(dir))
	if err != nil {
		t.Fatal(err)
	}

	if got := gitmodulesProblems(report); !slices.Equal(got, want) {
		t.Fatalf("problems\n ours %v\n git  %v", got, want)
	}
}
