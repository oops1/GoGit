package console

import (
	"context"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

var remoteListOptions = []option{
	plain("verbose", "v"),
}

var remoteURLOptions = []option{
	plain("push", ""),
}

func runRemote(_ context.Context, env Env, args []string) (string, error) {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "":
		return listRemotes(env, args)
	case "add":
		return addRemote(env, args)
	case "remove", "rm":
		return removeRemote(env, args)
	case "set-url":
		return setRemoteURL(env, args)
	}
	return "", detail(ErrUsage, sub)
}

func listRemotes(env Env, args []string) (string, error) {
	opts, err := parseOptions(args, remoteListOptions)
	if err != nil {
		return "", err
	}
	if rest := opts.args(); len(rest) > 0 {
		return "", detail(ErrUsage, rest[0])
	}
	var lines []string
	for _, rem := range remote.List(env.Repo.Config()) {
		if !opts.has("verbose") {
			lines = append(lines, rem.Name)
			continue
		}
		lines = append(lines, rem.Name+"\t"+rem.FetchURL()+" (fetch)", rem.Name+"\t"+rem.PushURL()+" (push)")
	}
	return strings.Join(lines, "\n"), nil
}

func addRemote(env Env, args []string) (string, error) {
	if len(args) != 2 {
		return "", detail(ErrUsage, "remote add")
	}
	if err := ops.AddRemote(env.Repo, args[0], args[1]); err != nil {
		return "", err
	}
	return "Added remote " + args[0], nil
}

func removeRemote(env Env, args []string) (string, error) {
	if len(args) != 1 {
		return "", detail(ErrUsage, "remote remove")
	}
	if err := ops.RemoveRemote(env.Repo, args[0]); err != nil {
		return "", err
	}
	return "Removed remote " + args[0], nil
}

func setRemoteURL(env Env, args []string) (string, error) {
	opts, err := parseOptions(args, remoteURLOptions)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	if len(rest) != 2 {
		return "", detail(ErrUsage, "remote set-url")
	}
	if err := ops.SetRemoteURL(env.Repo, rest[0], rest[1], opts.has("push")); err != nil {
		return "", err
	}
	return "Set the URL of " + rest[0], nil
}
