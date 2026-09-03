package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	fallbackBranch   = "master"
	defaultBranchKey = "init.defaultbranch"
	directoryMode    = 0o777
	fileMode         = 0o666
	headRefPrefix    = "ref: refs/heads/"
	descriptionText  = "Unnamed repository; edit this file 'description' to name the repository.\n"
	excludeText      = "# git ls-files --others --exclude-from=.git/info/exclude\n" +
		"# Lines that start with '#' are comments.\n" +
		"# For a project mostly in C, the following would be a good set of\n" +
		"# exclude patterns (uncomment them if you want to use them):\n" +
		"# *.[oa]\n" +
		"# *~\n"
)

var initTree = []string{"objects/info", packDirName, "refs/heads", "refs/tags", "info", hooksDirName}

type InitOptions struct {
	Bare           bool
	SeparateGitDir string
	InitialBranch  string
	Env            func(string) string
	NoSystem       bool
	SystemFile     string
	GlobalFile     string
}

func (o InitOptions) openOptions() OpenOptions {
	return OpenOptions{Env: o.Env, NoSystem: o.NoSystem, SystemFile: o.SystemFile, GlobalFile: o.GlobalFile}
}

type configValue struct {
	key   string
	value string
}

func Init(path string, opts InitOptions) (*Repository, error) {
	target := absClean(path)
	branch, err := initialBranch(opts)
	if err != nil {
		return nil, err
	}
	gitDir, err := prepareGitDir(target, opts)
	if err != nil {
		return nil, err
	}
	if err := createTree(gitDir); err != nil {
		return nil, err
	}
	if err := writeSkeleton(gitDir, branch, opts.Bare); err != nil {
		return nil, err
	}
	layout := Layout{GitDir: gitDir, CommonDir: gitDir, Bare: opts.Bare}
	if !opts.Bare {
		layout.WorkTree = target
	}
	return OpenLayout(layout, opts.openOptions())
}

func prepareGitDir(target string, opts InitOptions) (string, error) {
	if err := ensureDirectory(target); err != nil {
		return "", err
	}
	if opts.Bare {
		if opts.SeparateGitDir != "" {
			return "", fmt.Errorf("%w: a bare repository cannot use a separate git directory", ErrInvalidPath)
		}
		return target, nil
	}
	dot := filepath.Join(target, dotGit)
	if opts.SeparateGitDir == "" {
		if resolved, err := readGitFile(dot); err == nil {
			return resolved, nil
		}
		return dot, nil
	}
	gitDir := absClean(opts.SeparateGitDir)
	if err := os.MkdirAll(gitDir, directoryMode); err != nil {
		return "", err
	}
	link := []byte(gitFilePrefix + " " + filepath.ToSlash(gitDir) + "\n")
	if err := os.WriteFile(dot, link, fileMode); err != nil {
		return "", err
	}
	return gitDir, nil
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(path, directoryMode)
	case err != nil:
		return fmt.Errorf("%w: %s: %w", ErrInvalidPath, path, err)
	case !info.IsDir():
		return fmt.Errorf("%w: %s is not a directory", ErrInvalidPath, path)
	}
	return nil
}

func createTree(gitDir string) error {
	if err := os.MkdirAll(gitDir, directoryMode); err != nil {
		return err
	}
	for _, rel := range initTree {
		if err := os.MkdirAll(filepath.Join(gitDir, filepath.FromSlash(rel)), directoryMode); err != nil {
			return err
		}
	}
	return nil
}

func writeSkeleton(gitDir, branch string, bare bool) error {
	root, err := os.OpenRoot(gitDir)
	if err != nil {
		return err
	}
	return errors.Join(fillSkeleton(root, gitDir, branch, bare), root.Close())
}

func fillSkeleton(root *os.Root, gitDir, branch string, bare bool) error {
	return errors.Join(
		writeHead(root, gitDir, branch),
		writeIfMissing(root, descriptionName, descriptionText),
		writeIfMissing(root, infoExcludeName, excludeText),
		writeInitConfig(root, bare),
	)
}

func writeHead(root *os.Root, gitDir, branch string) error {
	if validHeadRef(filepath.Join(gitDir, headFile)) {
		return nil
	}
	return root.WriteFile(headFile, []byte(headRefPrefix+branch+"\n"), fileMode)
}

func writeIfMissing(root *os.Root, name, text string) error {
	fh, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = fh.WriteString(text)
	return errors.Join(err, fh.Close())
}

func writeInitConfig(root *os.Root, bare bool) error {
	data, err := root.ReadFile(configFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := config.Parse(data)
	if err != nil {
		return err
	}
	var errs []error
	for _, value := range initConfigValues(bare) {
		errs = append(errs, file.Set(value.key, value.value))
	}
	errs = append(errs, root.WriteFile(configFile, file.Encode(), fileMode))
	return errors.Join(errs...)
}

func initConfigValues(bare bool) []configValue {
	values := []configValue{
		{"core.repositoryformatversion", "0"},
		{"core.filemode", boolText(defaultFileMode)},
		{"core.bare", boolText(bare)},
	}
	if !bare {
		values = append(values, configValue{"core.logallrefupdates", "true"})
	}
	return append(values, platformInitValues()...)
}

func boolText(on bool) string {
	if on {
		return "true"
	}
	return "false"
}

func initialBranch(opts InitOptions) (string, error) {
	name := opts.InitialBranch
	if name == "" {
		configured, err := configuredBranch(opts)
		if err != nil {
			return "", err
		}
		name = configured
	}
	if name == "" {
		name = fallbackBranch
	}
	if !validBranchName(name) {
		return "", fmt.Errorf("%w: %q is not a branch name", ErrInvalidPath, name)
	}
	return name, nil
}

func configuredBranch(opts InitOptions) (string, error) {
	cfg, err := config.Load(opts.openOptions().configOptions(""))
	if err != nil {
		return "", err
	}
	name, _ := cfg.Get(defaultBranchKey)
	return name, nil
}

func validBranchName(name string) bool {
	if name == "" || name == "@" {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	for _, forbidden := range []string{"..", "//", "@{", "/.", ".lock/"} {
		if strings.Contains(name, forbidden) {
			return false
		}
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c <= ' ' || c == 0x7f || strings.IndexByte("~^:?*[\\", c) >= 0 {
			return false
		}
	}
	return true
}
