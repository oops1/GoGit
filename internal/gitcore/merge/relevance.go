package merge

func relevantSources(base, side, other Snapshot, directories bool) func(path string) bool {
	located := map[string]bool{}
	if directories {
		located = relocatableDirectories(base, side, other)
	}
	return func(path string) bool {
		if other[path] != base[path] {
			return true
		}
		for dir, _ := splitPath(path); dir != ""; dir, _ = splitPath(dir) {
			if located[dir] {
				return true
			}
		}
		return false
	}
}

func relocatableDirectories(base, side, other Snapshot) map[string]bool {
	baseDirs, sideDirs := directoriesOf(base), directoriesOf(side)
	located := map[string]bool{}
	for path := range other {
		if _, existed := base[path]; existed {
			continue
		}
		for dir, _ := splitPath(path); dir != ""; dir, _ = splitPath(dir) {
			if baseDirs[dir] && !sideDirs[dir] {
				located[dir] = true
			}
		}
	}
	return located
}
