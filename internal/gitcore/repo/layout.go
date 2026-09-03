package repo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	dotGit           = ".git"
	gitFilePrefix    = "gitdir:"
	headFile         = "HEAD"
	configFile       = "config"
	commonDirFile    = "commondir"
	gitDirFile       = "gitdir"
	objectsDirName   = "objects"
	refsDirName      = "refs"
	descriptionName  = "description"
	infoExcludeName  = "info/exclude"
	hooksDirName     = "hooks"
	packDirName      = "objects/pack"
	indexName        = "index"
	maxHeadRefSize   = 256
	maxGitFileSize   = 1 << 20
	worktreesDirName = "worktrees"
)

type Layout struct {
	GitDir     string
	CommonDir  string
	WorkTree   string
	Bare       bool
	IsWorktree bool
}

func absClean(path string) string {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}
	return filepath.Clean(path)
}

func resolveFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func readLimited(path string, limit int64) ([]byte, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(fh, limit))
	return data, errors.Join(err, fh.Close())
}

func firstLine(data []byte) string {
	text := string(data)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimRight(text, "\r \t")
}

func isHexObjectID(text string) bool {
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	for i := 0; i < len(text); i++ {
		if !isHexDigit(text[i]) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func validHeadRef(path string) bool {
	data, err := readLimited(path, maxHeadRefSize)
	if err != nil {
		return false
	}
	text := firstLine(data)
	if target, ok := strings.CutPrefix(text, "ref:"); ok {
		return strings.HasPrefix(strings.TrimLeft(target, " \t"), refsDirName+"/")
	}
	return isHexObjectID(text)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func commonDirOf(gitDir string) (string, error) {
	path := filepath.Join(gitDir, commonDirFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return gitDir, nil
	}
	if err != nil {
		return "", err
	}
	line := firstLine(data)
	if line == "" {
		return "", fmt.Errorf("%w: %s carries no path", ErrInvalidGitDirFile, path)
	}
	return resolveFrom(gitDir, filepath.FromSlash(line)), nil
}

func gitDirectoryCommon(dir string) (string, bool) {
	if !validHeadRef(filepath.Join(dir, headFile)) {
		return "", false
	}
	common, err := commonDirOf(dir)
	if err != nil {
		return "", false
	}
	if !isDirectory(filepath.Join(common, objectsDirName)) || !isDirectory(filepath.Join(common, refsDirName)) {
		return "", false
	}
	return common, true
}

func isGitDirectory(dir string) bool {
	_, ok := gitDirectoryCommon(dir)
	return ok
}

func readGitFile(path string) (string, error) {
	data, err := readLimited(path, maxGitFileSize)
	if err != nil {
		return "", err
	}
	rest, ok := strings.CutPrefix(firstLine(data), gitFilePrefix)
	if !ok {
		return "", fmt.Errorf("%w: %s does not start with %q", ErrInvalidGitDirFile, path, gitFilePrefix)
	}
	target := strings.TrimSpace(rest)
	if target == "" {
		return "", fmt.Errorf("%w: %s names no directory", ErrInvalidGitDirFile, path)
	}
	resolved := resolveFrom(filepath.Dir(path), filepath.FromSlash(target))
	if !isGitDirectory(resolved) {
		return "", fmt.Errorf("%w: %s points at %s", ErrInvalidGitDirFile, path, resolved)
	}
	return resolved, nil
}

func IsRepository(path string) bool {
	dir := absClean(path)
	dot := filepath.Join(dir, dotGit)
	if isGitDirectory(dot) {
		return true
	}
	if _, err := readGitFile(dot); err == nil {
		return true
	}
	return isGitDirectory(dir)
}
