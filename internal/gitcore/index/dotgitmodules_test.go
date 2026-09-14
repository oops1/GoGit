package index

import "testing"

func TestIsDotGitmodulesRecognisesEveryNameGitTreatsAsTheFile(t *testing.T) {
	for name, want := range map[string]bool{
		".gitmodules":    true,
		".GitModules":    true,
		".gitmodules .":  true,
		".git‌modules":   true,
		"gitmod~1":       true,
		"GI7EBA~1":       true,
		"gitmodules":     false,
		".gitmodulesx":   false,
		"sub.gitmodules": false,
		".gitmodules‌x":  false,
	} {
		if got := IsDotGitmodules(name); got != want {
			t.Errorf("IsDotGitmodules(%q) = %v, want %v", name, got, want)
		}
	}
}
