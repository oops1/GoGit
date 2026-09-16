//go:build oracle

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

const hookClock = 1700000000

func requireScriptHooks(t *testing.T) *oracle {
	t.Helper()
	if !hooks.CanRunScripts() {
		t.Skip("shell script hooks need Git for Windows")
	}
	return newOracle(t)
}

func (o *oracle) dated(clock int64) *oracle {
	stamp := strconv.FormatInt(clock, 10) + " +0000"
	env := append(slices.Clone(o.env), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	return &oracle{t: o.t, home: o.home, env: env}
}

func hookWhen(clock int64) time.Time { return time.Unix(clock, 0).UTC() }

func copyRepository(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "ours")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("CopyFS returned error %v", err)
	}
	return dst
}

func installOracleHooks(t *testing.T, dir string, h testHook, names ...string) {
	t.Helper()
	for _, name := range names {
		installShellHook(t, filepath.Join(dir, ".git", "hooks"), name, h)
	}
}

func sameHookRecords(t *testing.T, gitLog, ourLog string) {
	t.Helper()
	want, got := readHookLog(t, gitLog), readHookLog(t, ourLog)
	if !recordsEqual(got, want) {
		t.Fatalf("our hooks ran as\n%+v\ngit ran them as\n%+v", got, want)
	}
}

func sameOutput(t *testing.T, o *oracle, gitDir, ourDir string, args ...string) {
	t.Helper()
	want, _ := o.attempt(gitDir, args...)
	got, _ := o.attempt(ourDir, args...)
	if want != got {
		t.Fatalf("git %s: ours\n%s\ngit\n%s", strings.Join(args, " "), got, want)
	}
}

func oracleHookRepo(o *oracle) string {
	dir := o.repoDir("git")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	return dir
}

func TestOracleCommitHooksMatchGit(t *testing.T) {
	o := requireScriptHooks(t)
	gitDir := oracleHookRepo(o)
	o.write(gitDir, "a.txt", "a\n")
	o.run(gitDir, "add", "a.txt")
	ourDir := copyRepository(t, gitDir)
	gitLog, ourLog := hookLogPath(t), hookLogPath(t)
	for dir, log := range map[string]string{gitDir: gitLog, ourDir: ourLog} {
		installOracleHooks(t, dir, testHook{log: log}, hookPreCommit, hookPrepareCommitMsg, hookPostCommit)
		installOracleHooks(t, dir, testHook{log: log, appendTo: "Change-Id: I0123456789abcdef"}, hookCommitMsg)
		installOracleHooks(t, dir, testHook{log: log, stdin: true}, hookPostRewrite)
	}

	o.dated(hookClock).run(gitDir, "commit", "-q", "-m", "subject")
	o.dated(hookClock+60).run(gitDir, "commit", "-q", "--amend", "-m", "amended")
	r := o.openRepo(ourDir)
	if _, err := Commit(t.Context(), r, CommitOptions{Message: "subject", When: hookWhen(hookClock)}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if _, err := Commit(t.Context(), r, CommitOptions{Message: "amended", Amend: true, When: hookWhen(hookClock + 60)}); err != nil {
		t.Fatalf("amending Commit returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	sameHookRecords(t, gitLog, ourLog)
	sameOutput(t, o, gitDir, ourDir, "cat-file", "commit", "HEAD")
	sameOutput(t, o, gitDir, ourDir, "rev-parse", "HEAD")
}

func TestOracleRejectingPreCommitHookMatchesGit(t *testing.T) {
	o := requireScriptHooks(t)
	gitDir := oracleHookRepo(o)
	o.write(gitDir, "a.txt", "a\n")
	o.run(gitDir, "add", "a.txt")
	ourDir := copyRepository(t, gitDir)
	gitLog, ourLog := hookLogPath(t), hookLogPath(t)
	for dir, log := range map[string]string{gitDir: gitLog, ourDir: ourLog} {
		installOracleHooks(t, dir, testHook{log: log, exit: 1}, hookPreCommit)
		installOracleHooks(t, dir, testHook{log: log}, hookPrepareCommitMsg, hookCommitMsg, hookPostCommit)
	}

	if _, err := o.attempt(gitDir, "commit", "-q", "-m", "subject"); err == nil {
		t.Fatal("git commit succeeded despite the pre-commit hook")
	}
	if _, err := Commit(t.Context(), o.openRepo(ourDir), CommitOptions{Message: "subject"}); !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("Commit returned %v, want the pre-commit rejection", err)
	}

	sameHookRecords(t, gitLog, ourLog)
	sameOutput(t, o, gitDir, ourDir, "rev-parse", "--verify", "-q", "HEAD")
	sameOutput(t, o, gitDir, ourDir, "status", "--porcelain")
}

func TestOraclePrePushHookReceivesTheSameInputAsGit(t *testing.T) {
	o := requireScriptHooks(t)
	dir := oracleHookRepo(o)
	o.write(dir, "a.txt", "1\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "one")
	o.run(dir, "branch", "old")
	gitBare, ourBare := o.repoDir("git.git"), o.repoDir("ours.git")
	for _, bare := range []string{gitBare, ourBare} {
		o.run(bare, "init", "-q", "--bare")
		o.run(dir, "push", "-q", bare, "main", "old")
	}
	o.write(dir, "a.txt", "2\n")
	o.run(dir, "commit", "-q", "-am", "two")
	o.run(dir, "tag", "-a", "-m", "v2", "v2")
	o.run(dir, "branch", "topic")
	o.run(dir, "remote", "add", "origin", gitBare)
	log := hookLogPath(t)
	installOracleHooks(t, dir, testHook{log: log, stdin: true}, hookPrePush)

	o.run(dir, "push", "-q", "--follow-tags", "origin", "topic", ":old", "main")
	gitRecords := readHookLog(t, log)
	if err := os.Remove(log); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	for _, tracking := range []string{"refs/remotes/origin/main", "refs/remotes/origin/topic"} {
		o.run(dir, "update-ref", "-d", tracking)
	}
	o.run(dir, "remote", "set-url", "origin", ourBare)
	specs := mustPushSpecs(t, "refs/heads/topic:refs/heads/topic", ":refs/heads/old", "refs/heads/main:refs/heads/main")
	if _, err := Push(t.Context(), o.openRepo(dir), "origin", remote.PushOptions{Refspecs: specs, FollowTags: true}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	ourRecords := readHookLog(t, log)

	for _, pair := range []struct {
		records []hookRecord
		url     string
	}{{gitRecords, gitBare}, {ourRecords, ourBare}} {
		if len(pair.records) != 1 || len(pair.records[0].args) != 2 || pair.records[0].args[1] != pair.url {
			t.Fatalf("pre-push records = %+v, want one run for %s", pair.records, pair.url)
		}
		pair.records[0].args[1] = "url"
	}
	if !recordsEqual(ourRecords, gitRecords) {
		t.Fatalf("our pre-push got\n%+v\ngit gave\n%+v", ourRecords, gitRecords)
	}
	if want, got := o.run(gitBare, "show-ref"), o.run(ourBare, "show-ref"); want != got {
		t.Fatalf("our server holds\n%s\ngit's holds\n%s", got, want)
	}
}

func TestOraclePostCheckoutArgumentsMatchGit(t *testing.T) {
	o := requireScriptHooks(t)
	gitDir := oracleHookRepo(o)
	o.write(gitDir, "a.txt", "1\n")
	o.run(gitDir, "add", ".")
	o.dated(hookClock).run(gitDir, "commit", "-q", "-m", "one")
	o.run(gitDir, "checkout", "-q", "-b", "topic")
	o.write(gitDir, "b.txt", "2\n")
	o.run(gitDir, "add", ".")
	o.dated(hookClock+60).run(gitDir, "commit", "-q", "-m", "two")
	o.run(gitDir, "checkout", "-q", "main")
	topic := strings.TrimSpace(o.run(gitDir, "rev-parse", "topic"))
	ourDir := copyRepository(t, gitDir)
	gitLog, ourLog := hookLogPath(t), hookLogPath(t)
	for dir, log := range map[string]string{gitDir: gitLog, ourDir: ourLog} {
		installOracleHooks(t, dir, testHook{log: log}, hookPostCheckout)
	}

	for _, args := range [][]string{{"topic"}, {"main"}, {"--detach", "topic"}, {"-b", "fresh"}} {
		o.run(gitDir, append([]string{"checkout", "-q"}, args...)...)
	}
	r := o.openRepo(ourDir)
	for _, target := range []string{"topic", "main", topic} {
		if err := Switch(t.Context(), r, target, SwitchOptions{}); err != nil {
			t.Fatalf("Switch %s returned error %v", target, err)
		}
	}
	if _, err := StartBranch(t.Context(), r, "fresh", "", StartBranchOptions{}); err != nil {
		t.Fatalf("StartBranch returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	sameHookRecords(t, gitLog, ourLog)
}

type hookMergeScenario struct {
	name     string
	gitArgs  []string
	opts     MergeOptions
	conflict bool
	ahead    bool
}

func TestOracleMergeHooksMatchGit(t *testing.T) {
	scenarios := []hookMergeScenario{
		{name: "merge commit", gitArgs: []string{"topic"}},
		{name: "no verify", gitArgs: []string{"--no-verify", "topic"}, opts: MergeOptions{Hooks: HookOptions{NoVerify: true}}},
		{name: "squash", gitArgs: []string{"--squash", "topic"}, opts: MergeOptions{Mode: MergeSquash}},
		{name: "no commit", gitArgs: []string{"--no-commit", "topic"}, opts: MergeOptions{NoCommit: true}},
		{name: "fast-forward", gitArgs: []string{"topic"}, ahead: true},
		{name: "conflict", gitArgs: []string{"topic"}, conflict: true},
		{name: "squash conflict", gitArgs: []string{"--squash", "topic"}, opts: MergeOptions{Mode: MergeSquash}, conflict: true},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			o := requireScriptHooks(t)
			gitDir := oracleHookRepo(o)
			o.write(gitDir, "f.txt", "base\n")
			o.run(gitDir, "add", ".")
			o.dated(hookClock).run(gitDir, "commit", "-q", "-m", "base")
			o.run(gitDir, "checkout", "-q", "-b", "topic")
			if s.conflict {
				o.write(gitDir, "f.txt", "topic\n")
			} else {
				o.write(gitDir, "t.txt", "topic\n")
			}
			o.run(gitDir, "add", ".")
			o.dated(hookClock+60).run(gitDir, "commit", "-q", "-m", "topic")
			o.run(gitDir, "checkout", "-q", "main")
			if !s.ahead {
				o.write(gitDir, "f.txt", "main\n")
				o.run(gitDir, "add", ".")
				o.dated(hookClock+120).run(gitDir, "commit", "-q", "-m", "main")
			}
			ourDir := copyRepository(t, gitDir)
			gitLog, ourLog := hookLogPath(t), hookLogPath(t)
			for dir, log := range map[string]string{gitDir: gitLog, ourDir: ourLog} {
				installOracleHooks(t, dir, testHook{log: log}, hookPreMergeCommit, hookPrepareCommitMsg, hookPostMerge, hookPostCheckout)
				installOracleHooks(t, dir, testHook{log: log, appendTo: "Change-Id: I89abcdef"}, hookCommitMsg)
			}

			_, _ = o.dated(hookClock+180).attempt(gitDir, append([]string{"merge", "-q"}, s.gitArgs...)...)
			opts := s.opts
			opts.When = hookWhen(hookClock + 180)
			r := o.openRepo(ourDir)
			if _, err := Merge(t.Context(), r, "topic", opts); err != nil {
				t.Fatalf("Merge returned error %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatalf("Close returned error %v", err)
			}

			sameHookRecords(t, gitLog, ourLog)
			sameOutput(t, o, gitDir, ourDir, "cat-file", "commit", "HEAD")
			sameOutput(t, o, gitDir, ourDir, "rev-parse", "HEAD")
		})
	}
}

func rebaseHookRepos(t *testing.T, o *oracle) (string, string) {
	t.Helper()
	gitDir := oracleHookRepo(o)
	o.write(gitDir, "f.txt", "base\n")
	o.run(gitDir, "add", ".")
	o.dated(hookClock).run(gitDir, "commit", "-q", "-m", "base")
	o.run(gitDir, "checkout", "-q", "-b", "topic")
	for i, name := range []string{"t.txt", "u.txt"} {
		o.write(gitDir, name, name+"\n")
		o.run(gitDir, "add", ".")
		o.dated(hookClock+int64(60*(i+1))).run(gitDir, "commit", "-q", "-m", name)
	}
	o.run(gitDir, "checkout", "-q", "main")
	o.write(gitDir, "m.txt", "main\n")
	o.run(gitDir, "add", ".")
	o.dated(hookClock+180).run(gitDir, "commit", "-q", "-m", "main")
	o.run(gitDir, "checkout", "-q", "topic")
	return gitDir, copyRepository(t, gitDir)
}

func TestOracleRebaseHooksMatchGit(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run("refuse="+strconv.FormatBool(refuse), func(t *testing.T) {
			o := requireScriptHooks(t)
			gitDir, ourDir := rebaseHookRepos(t, o)
			gitLog, ourLog := hookLogPath(t), hookLogPath(t)
			exit := 0
			if refuse {
				exit = 1
			}
			for dir, log := range map[string]string{gitDir: gitLog, ourDir: ourLog} {
				installOracleHooks(t, dir, testHook{log: log, exit: exit}, hookPreRebase)
				installOracleHooks(t, dir, testHook{log: log}, hookPostCheckout)
				installOracleHooks(t, dir, testHook{log: log, stdin: true}, hookPostRewrite)
			}

			_, gitErr := o.dated(hookClock+240).attempt(gitDir, "rebase", "main")
			_, err := Rebase(t.Context(), o.openRepo(ourDir), "main", RebaseOptions{When: hookWhen(hookClock + 240)})
			if refuse != (gitErr != nil) || refuse != errors.Is(err, hooks.ErrRejected) {
				t.Fatalf("git rebase error %v, our error %v", gitErr, err)
			}

			sameHookRecords(t, gitLog, ourLog)
			sameOutput(t, o, gitDir, ourDir, "rev-parse", "HEAD")
			sameOutput(t, o, gitDir, ourDir, "symbolic-ref", "HEAD")
		})
	}
}
