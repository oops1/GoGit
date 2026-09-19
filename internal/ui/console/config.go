package console

import (
	"context"
	"errors"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

var ErrNoLocalConfig = errors.New("console: the repository has no local configuration file")

var saveConfig = func(file *config.File) error { return file.Save(file.Path()) }

var configOptions = []option{
	plain("list", "l"),
	valued("get", ""),
	valued("unset", ""),
	plain("local", ""),
}

func runConfig(_ context.Context, env Env, args []string) (string, error) {
	opts, err := parseOptions(args, configOptions)
	if err != nil {
		return "", err
	}
	rest := opts.args()
	switch {
	case opts.has("list"):
		return listConfig(env), nil
	case opts.has("unset"):
		return unsetConfig(env, opts.value("unset"))
	case opts.has("get"):
		return readConfig(env, opts.value("get"))
	case len(rest) == 1:
		return readConfig(env, rest[0])
	case len(rest) == 2:
		return writeConfig(env, rest[0], rest[1])
	}
	return "", detail(ErrUsage, "config")
}

func listConfig(env Env) string {
	var lines []string
	for entry := range env.Repo.Config().All() {
		lines = append(lines, entry.Name()+"="+entry.Value)
	}
	return strings.Join(lines, "\n")
}

func readConfig(env Env, key string) (string, error) {
	values := env.Repo.Config().GetAll(key)
	if len(values) == 0 {
		return "", detail(ErrUsage, key)
	}
	return strings.Join(values, "\n"), nil
}

func localConfig(env Env) (*config.File, error) {
	file, ok := env.Repo.Config().File(config.LevelLocal)
	if !ok {
		return nil, ErrNoLocalConfig
	}
	return file, nil
}

func writeConfig(env Env, key, value string) (string, error) {
	file, err := localConfig(env)
	if err != nil {
		return "", err
	}
	if err := file.Set(key, value); err != nil {
		return "", err
	}
	if err := saveConfig(file); err != nil {
		return "", err
	}
	return key + "=" + value, nil
}

func unsetConfig(env Env, key string) (string, error) {
	file, err := localConfig(env)
	if err != nil {
		return "", err
	}
	if err := file.UnsetAll(key); err != nil {
		return "", err
	}
	if err := saveConfig(file); err != nil {
		return "", err
	}
	return "Unset " + key, nil
}
