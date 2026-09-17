package ops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/submodule"
)

var (
	ErrFetchRecurseSetting   = errors.New("ops: invalid submodule fetch recursion setting")
	ErrSubmoduleInaccessible = errors.New("ops: could not access the submodule")
)

type SubmoduleFetchMode int

const (
	SubmoduleFetchConfigured SubmoduleFetchMode = iota
	SubmoduleFetchOn
	SubmoduleFetchOff
	SubmoduleFetchOnDemand
)

type SubmoduleFetchOptions struct {
	Mode    SubmoduleFetchMode
	Default SubmoduleFetchMode
	Events  SubmoduleEvents

	prefix string
}

type changedSubmodule struct {
	module  submodule.Module
	known   bool
	path    string
	commits []hash.ObjectID
}

func FetchRecursive(ctx context.Context, r *repo.Repository, remoteName string, opts remote.FetchOptions, sub SubmoduleFetchOptions) (remote.FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return remote.FetchResult{}, err
	}
	cfg := r.Config()
	name, err := resolveRemoteName(r, cfg, remoteName)
	if err != nil {
		return remote.FetchResult{}, err
	}
	rem, err := remote.Load(cfg, name)
	if err != nil {
		return remote.FetchResult{}, err
	}
	return fetchWithSubmodules(ctx, r, rem, opts, sub)
}

func fetchWithSubmodules(ctx context.Context, r *repo.Repository, rem remote.Remote, opts remote.FetchOptions, sub SubmoduleFetchOptions) (remote.FetchResult, error) {
	mode, err := fetchRecurseMode(r.Config(), sub.Mode)
	if err != nil {
		return remote.FetchResult{}, err
	}
	recurse := mode != SubmoduleFetchOff && mayHaveSubmodules(r)
	var before []hash.ObjectID
	if recurse {
		before = refTipsOf(r)
	}
	result, err := fetchRemote(ctx, r, rem, opts)
	if err != nil || !recurse {
		return result, err
	}
	return result, fetchSubmodules(ctx, r, before, result, mode, opts, sub)
}

func mayHaveSubmodules(r *repo.Repository) bool {
	if r.IsBare() || r.WorkTree() == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(r.WorkTree(), submodule.GitmodulesFile)); err == nil {
		return true
	}
	idx, err := submoduleReadIndex(r.IndexFile())
	if err != nil {
		return false
	}
	for entry := range idx.Entries() {
		if entry.Mode.IsSubmodule() || entry.Path == submodule.GitmodulesFile {
			return true
		}
	}
	return false
}

func fetchRecurseMode(cfg *config.Config, explicit SubmoduleFetchMode) (SubmoduleFetchMode, error) {
	if explicit != SubmoduleFetchConfigured {
		return explicit, nil
	}
	mode := SubmoduleFetchConfigured
	for entry := range cfg.All() {
		if entry.HasSubsection {
			continue
		}
		var value submodule.FetchRecurse
		switch {
		case entry.Section == "submodule" && entry.Key == "recurse":
			value = submodule.ParseFetchRecurse(entry.Value, entry.HasValue)
			if value == submodule.FetchRecurseOnDemand {
				value = submodule.FetchRecurseInvalid
			}
		case entry.Section == "fetch" && entry.Key == "recursesubmodules":
			value = submodule.ParseFetchRecurse(entry.Value, entry.HasValue)
		default:
			continue
		}
		parsed, err := fetchModeOf(value)
		if err != nil {
			return mode, fmt.Errorf("%w: %s = %q", err, entry.Name(), entry.Value)
		}
		mode = parsed
	}
	return mode, nil
}

func fetchModeOf(value submodule.FetchRecurse) (SubmoduleFetchMode, error) {
	switch value {
	case submodule.FetchRecurseOn:
		return SubmoduleFetchOn, nil
	case submodule.FetchRecurseOff:
		return SubmoduleFetchOff, nil
	case submodule.FetchRecurseOnDemand:
		return SubmoduleFetchOnDemand, nil
	}
	return SubmoduleFetchConfigured, ErrFetchRecurseSetting
}

func refTipsOf(r *repo.Repository) []hash.ObjectID {
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()
	tips, _ := refTips(r, db)
	return tips
}

func fetchSubmodules(ctx context.Context, r *repo.Repository, before []hash.ObjectID, result remote.FetchResult, mode SubmoduleFetchMode, opts remote.FetchOptions, sub SubmoduleFetchOptions) error {
	super, err := loadSuperproject(r, nil)
	if err != nil {
		return err
	}
	super.prefix = sub.prefix
	changed := map[string]*changedSubmodule{}
	if mode != SubmoduleFetchOn {
		if changed, err = super.changedSubmodules(ctx, before, result); err != nil {
			return err
		}
	}
	f := &submoduleFetcher{super: super, ctx: ctx, changed: changed, mode: mode, opts: opts, sub: sub, seen: map[string]bool{}}
	for _, link := range super.links {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.fromIndex(link)
	}
	for _, name := range slices.Sorted(maps.Keys(changed)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.fromChanged(changed[name])
	}
	return errors.Join(f.failures...)
}

func (s *superproject) changedSubmodules(ctx context.Context, before []hash.ObjectID, result remote.FetchResult) (map[string]*changedSubmodule, error) {
	changed := map[string]*changedSubmodule{}
	var after []hash.ObjectID
	for _, change := range result.Changes {
		if !change.Deleted && !change.New.IsZero() {
			after = append(after, change.New)
		}
	}
	if s.modules.Len() == 0 || len(after) == 0 {
		return changed, nil
	}
	db, err := odbOpen(s.repo.ObjectsDir(), odb.Options{Format: s.repo.ObjectFormat})
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	shallow, err := s.repo.Shallow()
	if err != nil {
		return nil, err
	}
	cache := map[hash.ObjectID]*submodule.Modules{}
	for commit, err := range revision.Walk(ctx, revision.Options{Context: revision.Context{Objects: db, Shallow: shallow}, Include: after, Exclude: before}) {
		if err != nil {
			return nil, err
		}
		if len(commit.Parents) == 0 {
			continue
		}
		links, err := commitGitlinkChanges(db, commit.Tree, commit.Parents)
		if err != nil {
			return nil, err
		}
		if len(links) == 0 {
			continue
		}
		modules, err := modulesInTree(db, commit.Tree, cache)
		if err != nil {
			return nil, err
		}
		for _, path := range slices.Sorted(maps.Keys(links)) {
			s.recordChange(changed, modules, path, links[path])
		}
	}
	for name, entry := range changed {
		if s.submoduleHasCommits(ctx, name, entry.commits) {
			delete(changed, name)
		}
	}
	return changed, nil
}

func (s *superproject) recordChange(changed map[string]*changedSubmodule, modules *submodule.Modules, path string, id hash.ObjectID) {
	module, known := modules.ByPath(path)
	if !known {
		if _, populated := populatedLayout(s.submoduleDir(path)); !populated {
			return
		}
		if _, collides := modules.ByName(path); collides {
			return
		}
		module = submodule.Module{Name: path, Path: path}
	}
	entry, ok := changed[module.Name]
	if !ok {
		entry = &changedSubmodule{module: module, known: known, path: path}
		changed[module.Name] = entry
	}
	entry.commits = append(entry.commits, id)
}

func commitGitlinkChanges(db *odb.DB, tree hash.ObjectID, parents []hash.ObjectID) (map[string]hash.ObjectID, error) {
	parentTrees := make([]hash.ObjectID, len(parents))
	for i, parent := range parents {
		commit, err := dbCommit(db, parent)
		if err != nil {
			return nil, err
		}
		parentTrees[i] = commit.Tree
	}
	out := map[string]hash.ObjectID{}
	return out, collectGitlinkChanges(db, tree, parentTrees, "", out)
}

func collectGitlinkChanges(db *odb.DB, tree hash.ObjectID, parents []hash.ObjectID, prefix string, out map[string]hash.ObjectID) error {
	current, err := dbTree(db, tree)
	if err != nil {
		return err
	}
	parentEntries := make([]map[string]treeEntry, len(parents))
	for i, parent := range parents {
		parentEntries[i] = map[string]treeEntry{}
		if parent.IsZero() {
			continue
		}
		parentTree, err := dbTree(db, parent)
		if err != nil {
			return err
		}
		for _, entry := range parentTree.Entries {
			parentEntries[i][entry.Name] = treeEntry{mode: entry.Mode, id: entry.ID}
		}
	}
	for _, entry := range current.Entries {
		if !entry.Mode.IsTree() && !entry.Mode.IsSubmodule() {
			continue
		}
		unchanged := false
		subParents := make([]hash.ObjectID, len(parents))
		for i := range parents {
			old, ok := parentEntries[i][entry.Name]
			unchanged = unchanged || ok && old.mode == entry.Mode && old.id == entry.ID
			if ok && old.mode.IsTree() {
				subParents[i] = old.id
			}
		}
		switch {
		case unchanged:
		case entry.Mode.IsSubmodule():
			out[joinRel(prefix, entry.Name)] = entry.ID
		default:
			if err := collectGitlinkChanges(db, entry.ID, subParents, joinRel(prefix, entry.Name), out); err != nil {
				return err
			}
		}
	}
	return nil
}

func modulesInTree(db *odb.DB, tree hash.ObjectID, cache map[hash.ObjectID]*submodule.Modules) (*submodule.Modules, error) {
	root, err := dbTree(db, tree)
	if err != nil {
		return nil, err
	}
	blob := hash.Zero
	for _, entry := range root.Entries {
		if entry.Name == submodule.GitmodulesFile && entry.Mode.IsRegular() {
			blob = entry.ID
		}
	}
	if modules, ok := cache[blob]; ok {
		return modules, nil
	}
	var data []byte
	if !blob.IsZero() {
		if _, data, err = dbGet(db, blob); err != nil {
			return nil, err
		}
	}
	modules, err := submodule.Parse(data)
	if err != nil {
		modules, _ = submodule.Parse(nil)
	}
	cache[blob] = modules
	return modules, nil
}

func (s *superproject) submoduleHasCommits(ctx context.Context, name string, commits []hash.ObjectID) bool {
	module, known := s.modules.ByName(name)
	if !known {
		module = submodule.Module{Name: name, Path: name}
	}
	if _, populated := populatedLayout(s.submoduleDir(module.Path)); !populated {
		return false
	}
	sub, ok := s.submoduleRepoFor(module.Path, module)
	if !ok {
		return false
	}
	defer func() { _ = sub.Close() }()
	for _, id := range commits {
		if !tipReachable(ctx, sub, id) {
			return false
		}
	}
	return true
}

type submoduleFetcher struct {
	super    *superproject
	ctx      context.Context
	changed  map[string]*changedSubmodule
	mode     SubmoduleFetchMode
	opts     remote.FetchOptions
	sub      SubmoduleFetchOptions
	seen     map[string]bool
	failures []error
}

func (f *submoduleFetcher) indexModule(path string) (submodule.Module, bool) {
	if module, ok := f.super.modules.ByPath(path); ok {
		return module, true
	}
	_, populated := populatedLayout(f.super.submoduleDir(path))
	return submodule.Module{Name: path, Path: path}, populated
}

func (f *submoduleFetcher) recurseModeFor(module submodule.Module) SubmoduleFetchMode {
	if f.mode != SubmoduleFetchConfigured {
		return f.mode
	}
	value := module.FetchRecurse
	if raw, ok := f.super.cfg.GetValue("submodule." + module.Name + ".fetchRecurseSubmodules"); ok {
		value = submodule.ParseFetchRecurse(raw, true)
	}
	if value == submodule.FetchRecurseUnset {
		return f.sub.Default
	}
	mode, _ := fetchModeOf(value)
	return mode
}

func (f *submoduleFetcher) taskMode(module submodule.Module) (SubmoduleFetchMode, bool) {
	if f.seen[module.Name] {
		return 0, false
	}
	switch mode := f.recurseModeFor(module); mode {
	case SubmoduleFetchOff:
		return mode, false
	case SubmoduleFetchOn:
		return mode, true
	}
	_, changed := f.changed[module.Name]
	return SubmoduleFetchOnDemand, changed
}

func (f *submoduleFetcher) fromIndex(link gitlinkEntry) {
	module, ok := f.indexModule(link.path)
	if !ok {
		return
	}
	mode, ok := f.taskMode(module)
	if !ok {
		return
	}
	sub, ok := f.super.submoduleRepoFor(link.path, module)
	if !ok {
		if entries, err := os.ReadDir(f.super.submoduleDir(link.path)); err == nil && len(entries) > 0 {
			f.failures = append(f.failures, fmt.Errorf("%w: %s", ErrSubmoduleInaccessible, f.super.displayPath(link.path)))
		}
		return
	}
	defer func() { _ = sub.Close() }()
	f.seen[module.Name] = true
	f.fetchInto(sub, module, link.path, mode)
}

func (f *submoduleFetcher) fromChanged(change *changedSubmodule) {
	if !change.known {
		return
	}
	active, err := submodule.Active(f.super.cfg, change.module)
	if err != nil || !active {
		return
	}
	if _, ok := f.taskMode(change.module); !ok {
		return
	}
	sub, ok := f.super.submoduleRepoFor(change.path, change.module)
	if !ok {
		return
	}
	defer func() { _ = sub.Close() }()
	f.fetchInto(sub, change.module, change.path, SubmoduleFetchOnDemand)
}

func (f *submoduleFetcher) fetchInto(sub *repo.Repository, module submodule.Module, path string, mode SubmoduleFetchMode) {
	display := f.super.displayPath(path)
	f.sub.Events.emit(SubmoduleEvent{Kind: SubmoduleFetching, Path: display, Name: module.Name})
	nested := SubmoduleFetchOptions{Default: mode, Events: f.sub.Events, prefix: display + "/"}
	fetchOpts := remote.FetchOptions{Progress: f.opts.Progress, Transport: f.opts.Transport}
	if _, err := FetchRecursive(f.ctx, sub, "", fetchOpts, nested); err != nil {
		f.failures = append(f.failures, fmt.Errorf("%w: %s: %w", ErrSubmoduleFetch, display, err))
		return
	}
	change, ok := f.changed[module.Name]
	if !ok {
		return
	}
	var missing []hash.ObjectID
	for _, id := range change.commits {
		if !hasObject(sub, id) && !slices.Contains(missing, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	f.sub.Events.emit(SubmoduleEvent{Kind: SubmoduleFetchRetry, Path: display, Name: module.Name, Commit: missing[0]})
	fetchOpts.Wants = missing
	if _, err := FetchRecursive(f.ctx, sub, defaultCloneRemoteName, fetchOpts, nested); err != nil {
		f.failures = append(f.failures, fmt.Errorf("%w: %s: %w", ErrSubmoduleFetch, display, err))
	}
}

func (s *superproject) submoduleRepoFor(path string, module submodule.Module) (*repo.Repository, bool) {
	dir := s.submoduleDir(path)
	if layout, populated := populatedLayout(dir); populated {
		sub, err := submoduleOpenLayout(layout, s.repo.Options())
		return sub, err == nil
	}
	gitDir := s.modulesGitDir(module.Name)
	if !repo.IsRepository(gitDir) {
		return nil, false
	}
	sub, err := submoduleOpenLayout(repo.Layout{GitDir: gitDir, CommonDir: gitDir, Bare: true}, s.repo.Options())
	return sub, err == nil
}
