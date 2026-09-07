//go:build linux

package credential

const helperExeSuffix = ""

var helperDirsRelativeToGit = []string{
	"libexec/git-core",
	"lib/git-core",
}

func fixedHelperDirs() []string {
	return []string{
		"/usr/libexec/git-core",
		"/usr/lib/git-core",
		"/usr/local/libexec/git-core",
		"/usr/local/lib/git-core",
	}
}
