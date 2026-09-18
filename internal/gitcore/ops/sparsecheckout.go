package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var (
	ErrNotSparse          = errors.New("ops: this worktree is not sparse")
	ErrConePatternNotPath = errors.New("ops: cone mode takes directories, not patterns")
)

const (
	sparseCheckoutKey     = "core.sparsecheckout"
	sparseCheckoutConeKey = "core.sparsecheckoutcone"
	sparseIndexKey        = "index.sparse"
	worktreeConfigKey     = "extensions.worktreeConfig"
	worktreeConfigFile    = "config.worktree"
	sparseFileRelative    = "info/sparse-checkout"
	sparseEverything      = "/*\n"
	sparseGlobCharacters  = "*?[]"
	sparseFileMode        = 0o666
)

var (
	sparseWriteFile = os.WriteFile
	sparseMkdirAll  = os.MkdirAll
)

type SparseCheckoutOptions struct {
	Cone       bool
	SkipChecks bool
}

func SparseCheckoutInit(ctx context.Context, r *repo.Repository, opts SparseCheckoutOptions) error {
	lines, err := readSparsePatterns(r)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		lines = sparseConeHeader()
	}
	return applySparseCheckout(ctx, r, lines, opts.Cone)
}

func SparseCheckoutSet(ctx context.Context, r *repo.Repository, patterns []string, opts SparseCheckoutOptions) error {
	lines, err := sparsePatternLines(patterns, opts)
	if err != nil {
		return err
	}
	return applySparseCheckout(ctx, r, lines, opts.Cone)
}

func SparseCheckoutAdd(ctx context.Context, r *repo.Repository, patterns []string, opts SparseCheckoutOptions) error {
	if err := requireSparse(r); err != nil {
		return err
	}
	cone, err := sparseConeEnabled(r)
	if err != nil {
		return err
	}
	opts.Cone = cone
	added, err := sparsePatternLines(patterns, opts)
	if err != nil {
		return err
	}
	existing, err := readSparsePatterns(r)
	if err != nil {
		return err
	}
	return applySparseCheckout(ctx, r, sparseMerged(existing, added, cone), cone)
}

func SparseCheckoutList(r *repo.Repository) ([]string, error) {
	if err := requireSparse(r); err != nil {
		return nil, err
	}
	cone, err := sparseConeEnabled(r)
	if err != nil {
		return nil, err
	}
	lines, err := readSparsePatterns(r)
	if err != nil {
		return nil, err
	}
	if !cone {
		return lines, nil
	}
	dirs := sparseRecursiveDirs(lines)
	listed := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		listed = append(listed, strings.TrimPrefix(dir, "/"))
	}
	return listed, nil
}

func SparseCheckoutReapply(ctx context.Context, r *repo.Repository) error {
	if err := requireSparse(r); err != nil {
		return err
	}
	cone, err := sparseConeEnabled(r)
	if err != nil {
		return err
	}
	lines, err := readSparsePatterns(r)
	if err != nil {
		return err
	}
	return applySparseCheckout(ctx, r, lines, cone)
}

func SparseCheckoutDisable(ctx context.Context, r *repo.Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeSparseConfig(r, sparseConfigValues(true, false, false)); err != nil {
		return err
	}
	everything := sparseCheckout{
		patterns:     attributes.ParseSparse([]byte(sparseEverything), attributes.SparseOptions{Cone: true}),
		clearPresent: true,
	}
	if err := withReopenedRepository(r, func(reopened *repo.Repository) error {
		return updateSparseWorkingTree(ctx, reopened, everything)
	}); err != nil {
		return err
	}
	return writeSparseConfig(r, sparseConfigValues(false, false, true))
}

func requireSparse(r *repo.Repository) error {
	on, err := configFlag(r.Config(), sparseCheckoutKey, false)
	if err != nil {
		return err
	}
	if !on {
		return ErrNotSparse
	}
	return nil
}

func sparseConeEnabled(r *repo.Repository) (bool, error) {
	return configFlag(r.Config(), sparseCheckoutConeKey, false)
}

func sparseConeHeader() []string {
	return []string{"/*", "!/*/"}
}

func sparseConfigValues(on, cone, disabling bool) [][2]string {
	values := [][2]string{
		{sparseCheckoutKey, sparseBoolText(on)},
		{sparseCheckoutConeKey, sparseBoolText(cone)},
	}
	if disabling {
		values = append(values, [2]string{sparseIndexKey, sparseBoolText(false)})
	}
	return values
}

func sparseBoolText(on bool) string {
	if on {
		return "true"
	}
	return "false"
}

func applySparseCheckout(ctx context.Context, r *repo.Repository, lines []string, cone bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeSparseConfig(r, sparseConfigValues(true, cone, false)); err != nil {
		return err
	}
	if err := writeSparsePatterns(r, lines); err != nil {
		return err
	}
	return withReopenedRepository(r, func(reopened *repo.Repository) error {
		state, err := sparseCheckoutOf(reopened)
		if err != nil {
			return err
		}
		return updateSparseWorkingTree(ctx, reopened, state)
	})
}

func withReopenedRepository(r *repo.Repository, run func(*repo.Repository) error) error {
	reopened, err := cloneRepoOpenLayout(r.Layout(), r.Options())
	if err != nil {
		return err
	}
	return errors.Join(run(reopened), reopened.Close())
}

func updateSparseWorkingTree(ctx context.Context, r *repo.Repository, state sparseCheckout) error {
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: db})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	_, head, err := currentHeadState(store)
	if err != nil {
		return err
	}
	rules, err := pathRulesOf(r)
	if err != nil {
		return err
	}
	tree, err := verifiedTreeEntries(db, head, rules)
	if err != nil {
		return err
	}
	return layoutWorkingTreeWith(ctx, r, wt, db, state, tree, tree, false, nil)
}

func writeSparseConfig(r *repo.Repository, values [][2]string) error {
	on, err := configFlag(r.Config(), worktreeConfigKey, false)
	if err != nil {
		return err
	}
	if !on {
		local, err := localConfigFile(r)
		if err != nil {
			return err
		}
		if err := local.Set(worktreeConfigKey, sparseBoolText(true)); err != nil {
			return err
		}
		if err := local.Save(local.Path()); err != nil {
			return err
		}
	}
	return editConfigFile(r.GitPath(worktreeConfigFile), func(file *config.File) error {
		var errs []error
		for _, value := range values {
			errs = append(errs, file.Set(value[0], value[1]))
		}
		return errors.Join(errs...)
	})
}

func sparseFilePath(r *repo.Repository) string {
	return r.GitPath(sparseFileRelative)
}

func readSparsePatterns(r *repo.Repository) ([]string, error) {
	data, err := sparseReadFile(sparseFilePath(r))
	if missingPath(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func writeSparsePatterns(r *repo.Repository, lines []string) error {
	var text strings.Builder
	for _, line := range lines {
		text.WriteString(line)
		text.WriteString("\n")
	}
	target := sparseFilePath(r)
	if err := sparseMkdirAll(filepath.Dir(target), directoryMode); err != nil {
		return err
	}
	return sparseWriteFile(target, []byte(text.String()), sparseFileMode)
}

func sparsePatternLines(patterns []string, opts SparseCheckoutOptions) ([]string, error) {
	if !opts.Cone {
		return slices.Clone(patterns), nil
	}
	dirs, err := sparseConeDirectories(patterns, opts.SkipChecks)
	if err != nil {
		return nil, err
	}
	return sparseConeLines(dirs), nil
}

func sparseMerged(existing, added []string, cone bool) []string {
	if !cone {
		return append(slices.Clone(existing), added...)
	}
	return sparseConeLines(append(sparseRecursiveDirs(existing), sparseRecursiveDirs(added)...))
}

func sparseConeDirectories(patterns []string, skipChecks bool) ([]string, error) {
	dirs := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		dir, err := sparseConeDirectory(pattern, skipChecks)
		if err != nil {
			return nil, err
		}
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

func sparseConeDirectory(pattern string, skipChecks bool) (string, error) {
	if !skipChecks && (strings.ContainsAny(pattern, sparseGlobCharacters) || strings.HasPrefix(pattern, "!")) {
		return "", fmt.Errorf("%w: %s", ErrConePatternNotPath, pattern)
	}
	cleaned := path.Clean("/" + strings.Trim(strings.TrimSpace(filepath.ToSlash(pattern)), "/"))
	if cleaned == "/" {
		return "", nil
	}
	return cleaned, nil
}

func sparseConeLines(dirs []string) []string {
	recursive := map[string]bool{}
	parents := map[string]bool{}
	for _, dir := range dirs {
		recursive[dir] = true
		for up := sparseParent(dir); up != ""; up = sparseParent(up) {
			parents[up] = true
		}
	}
	lines := sparseConeHeader()
	for _, dir := range sparseSortedKeys(parents) {
		if recursive[dir] || sparseUnderRecursive(dir, recursive) {
			continue
		}
		lines = append(lines, dir+"/", "!"+dir+"/*/")
	}
	for _, dir := range sparseSortedKeys(recursive) {
		if sparseUnderRecursive(dir, recursive) {
			continue
		}
		lines = append(lines, dir+"/")
	}
	return lines
}

func sparseSortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sparseUnderRecursive(dir string, recursive map[string]bool) bool {
	for up := sparseParent(dir); up != ""; up = sparseParent(up) {
		if recursive[up] {
			return true
		}
	}
	return false
}

func sparseParent(dir string) string {
	slash := strings.LastIndexByte(dir, '/')
	if slash <= 0 {
		return ""
	}
	return dir[:slash]
}

func sparseRecursiveDirs(lines []string) []string {
	parents := map[string]bool{}
	for _, line := range lines {
		if dir, ok := strings.CutSuffix(strings.TrimPrefix(line, "!"), "/*/"); ok && strings.HasPrefix(line, "!") {
			parents[dir] = true
		}
	}
	var dirs []string
	for _, line := range lines {
		dir, ok := strings.CutSuffix(line, "/")
		if !ok || !strings.HasPrefix(line, "/") || dir == "" || parents[dir] {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}
