package merge

import (
	"maps"
	"slices"
)

type DirectoryRenames int

const (
	DirectoryRenamesConflict DirectoryRenames = iota
	DirectoryRenamesApply
	DirectoryRenamesOff
)

func directoryRenamesOf(renames Renames, base, side, adder Snapshot) map[string]string {
	located := relocatableDirectories(base, side, adder)
	relevant := func(dir string) bool {
		for at := dir; at != ""; at, _ = splitPath(at) {
			if located[at] {
				return true
			}
		}
		return false
	}
	baseDirs, sideDirs := directoriesOf(base), directoriesOf(side)
	counts := map[string]map[string]int{}
	for from, to := range renames {
		oldDir, _ := splitPath(from)
		newDir, _ := splitPath(to)
		for first := true; oldDir != newDir && baseDirs[oldDir] && !sideDirs[oldDir] && relevant(oldDir); first = false {
			if located[oldDir] || first {
				if counts[oldDir] == nil {
					counts[oldDir] = map[string]int{}
				}
				counts[oldDir][newDir]++
			}
			oldParent, oldName := splitPath(oldDir)
			newParent, newName := splitPath(newDir)
			if newDir == "" || oldName != newName {
				break
			}
			oldDir, newDir = oldParent, newParent
		}
	}
	out := map[string]string{}
	for oldDir, targets := range counts {
		best, bestCount, tied := "", 0, false
		for newDir, count := range targets {
			switch {
			case count > bestCount:
				best, bestCount, tied = newDir, count, false
			case count == bestCount:
				tied = true
			}
		}
		if !tied {
			out[oldDir] = best
		}
	}
	return out
}

func renamedAncestor(path string, dirRenames map[string]string) (string, string, bool) {
	for dir, _ := splitPath(path); dir != ""; dir, _ = splitPath(dir) {
		if newDir, renamed := dirRenames[dir]; renamed {
			return dir, newDir, true
		}
	}
	return "", "", false
}

func underDirectory(dir, rest string) string {
	if dir == "" {
		return rest
	}
	return dir + "/" + rest
}

func (a *aligned) applyDirectoryRenames(opts TreeOptions) (Renames, Renames) {
	if opts.DirectoryRenames == DirectoryRenamesOff {
		return opts.OurRenames, opts.TheirRenames
	}
	ourDirs := directoryRenamesOf(opts.OurRenames, a.base, a.ours, a.theirs)
	theirDirs := directoryRenamesOf(opts.TheirRenames, a.base, a.theirs, a.ours)
	flag := opts.DirectoryRenames == DirectoryRenamesConflict
	ours := a.moveIntoRenamedDirectories(a.ours, opts.OurRenames, theirDirs, ourDirs, flag)
	theirs := a.moveIntoRenamedDirectories(a.theirs, opts.TheirRenames, ourDirs, theirDirs, flag)
	return ours, theirs
}

func (a *aligned) moveIntoRenamedDirectories(side Snapshot, renames Renames, dirRenames, exclusions map[string]string, flag bool) Renames {
	renames = maps.Clone(renames)
	if len(dirRenames) == 0 {
		return renames
	}
	sources := map[string]string{}
	for from, to := range renames {
		sources[to] = from
	}
	arrivals := map[string]int{}
	moves := map[string]string{}
	for path := range side {
		if _, existed := a.base[path]; existed {
			continue
		}
		oldDir, newDir, renamed := renamedAncestor(path, dirRenames)
		if !renamed {
			continue
		}
		newPath := underDirectory(newDir, path[len(oldDir)+1:])
		arrivals[newPath]++
		if _, excluded := exclusions[newDir]; !excluded {
			moves[path] = newPath
		}
	}
	sideDirs := directoriesOf(side)
	for _, path := range slices.Sorted(maps.Keys(moves)) {
		newPath := moves[path]
		if arrivals[newPath] > 1 || a.inTheWay(newPath, side, sideDirs) {
			continue
		}
		side[newPath] = side[path]
		delete(side, path)
		if from, renamed := sources[path]; renamed {
			renames[from] = newPath
		}
		if flag {
			a.located[newPath] = true
		}
	}
	return renames
}

func (a *aligned) inTheWay(path string, side Snapshot, sideDirs map[string]bool) bool {
	if _, taken := side[path]; taken || sideDirs[path] {
		return true
	}
	base, ours, theirs := lookup(a.base, path), lookup(a.ours, path), lookup(a.theirs, path)
	return base != nil && ours != nil && theirs != nil && (same(ours, theirs) || same(base, ours) || same(base, theirs))
}
