package console

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

type Env struct {
	Repo      *gitrepo.Repository
	Now       time.Time
	Transport func(progress.Func) transport.Options
}

func (e Env) when() time.Time {
	if e.Now.IsZero() {
		return time.Now()
	}
	return e.Now
}

func (e Env) network() transport.Options {
	if e.Transport == nil {
		return transport.Options{}
	}
	return e.Transport(nil)
}

var openRefsStore = refs.Open

var openObjectsDB = odb.Open

func (e Env) openRefs() (*refs.Store, error) {
	return openRefsStore(refs.Options{
		GitDir:    e.Repo.GitDir(),
		CommonDir: e.Repo.CommonDir(),
		Bare:      e.Repo.IsBare(),
	})
}

func (e Env) openObjects() (*odb.DB, error) {
	return openObjectsDB(e.Repo.ObjectsDir(), odb.Options{Format: e.Repo.ObjectFormat})
}

type runner func(ctx context.Context, env Env, args []string) (string, error)

var commands = map[string]runner{
	"add":       runAdd,
	"branch":    runBranch,
	"checkout":  runCheckout,
	"commit":    runCommit,
	"config":    runConfig,
	"diff":      runDiff,
	"fetch":     runFetch,
	"log":       runLog,
	"merge":     runMerge,
	"pull":      runPull,
	"push":      runPush,
	"rebase":    runRebase,
	"remote":    runRemote,
	"reset":     runReset,
	"stage":     runAdd,
	"stash":     runStash,
	"status":    runStatus,
	"submodule": runSubmodule,
	"switch":    runSwitch,
	"tag":       runTag,
}

var unsupportedCommands = []string{
	"am",
	"apply",
	"bisect",
	"blame",
	"bundle",
	"cherry-pick",
	"clean",
	"clone",
	"describe",
	"fsck",
	"gc",
	"grep",
	"init",
	"ls-files",
	"mv",
	"notes",
	"reflog",
	"restore",
	"revert",
	"rm",
	"show",
	"sparse-checkout",
	"worktree",
}

func Names() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func Run(ctx context.Context, env Env, line string) (string, error) {
	cmd, err := Parse(line)
	if err != nil {
		return "", err
	}
	if cmd.Empty() {
		return "", nil
	}
	if cmd.Name == "help" {
		return strings.Join(Names(), "\n"), nil
	}
	fn, ok := commands[cmd.Name]
	if !ok {
		if slices.Contains(unsupportedCommands, cmd.Name) {
			return "", detail(ErrUnsupported, cmd.Name)
		}
		return "", detail(ErrUnknownCommand, cmd.Name)
	}
	if env.Repo == nil {
		return "", ErrNoRepository
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fn(ctx, env, cmd.Args)
}
