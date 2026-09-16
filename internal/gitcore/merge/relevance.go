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

func relocatableDirectories(base, renamer, adder Snapshot) map[string]bool {
	baseDirs, renamerDirs := directoriesOf(base), directoriesOf(renamer)
	located := map[string]bool{}
	for path := range adder {
		if _, existed := base[path]; existed {
			continue
		}
		if dir, _ := splitPath(path); baseDirs[dir] && !renamerDirs[dir] {
			located[dir] = true
		}
	}
	return located
}
