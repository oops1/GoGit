package repo

import (
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
)

func TestCommitGraphSettingsFollowConfigAndHistoryRewrites(t *testing.T) {
	replaceID := "1111111111111111111111111111111111111111"
	cases := []struct {
		name    string
		config  string
		files   map[string]string
		vars    env
		enabled bool
		version int
		open    commitgraph.OpenOptions
	}{
		{name: "defaults", enabled: true, version: 2},
		{name: "disabled", config: "[core]\n\tcommitGraph = false\n", version: 2},
		{name: "levels only", config: "[commitGraph]\n\tgenerationVersion = 1\n\treadChangedPaths = false\n", enabled: true, version: 1,
			open: commitgraph.OpenOptions{SkipGenerationData: true, SkipChangedPaths: true}},
		{name: "loose replace ref", files: map[string]string{"refs/replace/" + replaceID: replaceID + "\n"}, version: 2},
		{name: "packed replace ref", files: map[string]string{"packed-refs": replaceID + " refs/replace/" + replaceID + "\n"}, version: 2},
		{name: "packed refs without replacements", files: map[string]string{"packed-refs": replaceID + " refs/heads/main\n"}, enabled: true, version: 2},
		{name: "replace refs switched off", config: "[core]\n\tuseReplaceRefs = false\n", files: map[string]string{"refs/replace/" + replaceID: replaceID + "\n"}, enabled: true, version: 2},
		{name: "replace refs ignored by the environment", vars: env{noReplaceObjectsEnv: "1"}, files: map[string]string{"refs/replace/" + replaceID: replaceID + "\n"}, enabled: true, version: 2},
		{name: "only comments in grafts", files: map[string]string{graftsFileName: "# nothing\n\n"}, enabled: true, version: 2},
		{name: "grafts", files: map[string]string{graftsFileName: replaceID + "\n"}, version: 2},
		{name: "custom graft file", files: map[string]string{"custom-grafts": replaceID + "\n"}, vars: env{graftFileEnv: "custom-grafts"}, version: 2},
		{name: "shallow", files: map[string]string{shallowFileName: replaceID + "\n"}, version: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work := makeDir(t, filepath.Join(tempDir(t), "work"))
			gitDir := plainGitDir(t, filepath.Join(work, dotGit))
			writeFile(t, filepath.Join(gitDir, configFile), c.config)
			vars := env{}
			for key, value := range c.vars {
				vars[key] = value
			}
			for rel, text := range c.files {
				writeFile(t, filepath.Join(gitDir, filepath.FromSlash(rel)), text)
			}
			if custom, ok := vars[graftFileEnv]; ok {
				vars[graftFileEnv] = filepath.Join(gitDir, custom)
			}
			settings, err := openRepo(t, work, openOptions(t, vars)).CommitGraphSettings()
			if err != nil || settings.Enabled != c.enabled || settings.GenerationVersion != c.version || settings.Open != c.open {
				t.Fatalf("CommitGraphSettings() = %+v, %v", settings, err)
			}
		})
	}
}

func TestCommitGraphSettingsReportBrokenInputs(t *testing.T) {
	cases := []struct {
		name   string
		config string
		files  map[string]string
		dirs   []string
	}{
		{name: "core.commitGraph", config: "[core]\n\tcommitGraph = maybe\n"},
		{name: "readChangedPaths", config: "[commitGraph]\n\treadChangedPaths = maybe\n"},
		{name: "generationVersion", config: "[commitGraph]\n\tgenerationVersion = two\n"},
		{name: "useReplaceRefs", config: "[core]\n\tuseReplaceRefs = maybe\n"},
		{name: "packed refs", dirs: []string{"packed-refs"}},
		{name: "grafts", dirs: []string{graftsFileName}},
		{name: "shallow", files: map[string]string{shallowFileName: "not a hash\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work := makeDir(t, filepath.Join(tempDir(t), "work"))
			gitDir := plainGitDir(t, filepath.Join(work, dotGit))
			writeFile(t, filepath.Join(gitDir, configFile), c.config)
			for rel, text := range c.files {
				writeFile(t, filepath.Join(gitDir, filepath.FromSlash(rel)), text)
			}
			for _, rel := range c.dirs {
				makeDir(t, filepath.Join(gitDir, filepath.FromSlash(rel)))
			}
			if _, err := openRepo(t, work, openOptions(t, env{})).CommitGraphSettings(); err == nil {
				t.Fatal("CommitGraphSettings accepted a broken input")
			}
		})
	}
}

func TestCommitGraphSettingsReadTheProcessEnvironmentByDefault(t *testing.T) {
	work := makeDir(t, filepath.Join(tempDir(t), "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	t.Setenv(graftFileEnv, filepath.Join(work, "missing-grafts"))
	options := openOptions(t, env{})
	options.Env = nil
	settings, err := openRepo(t, work, options).CommitGraphSettings()
	if err != nil || !settings.Enabled {
		t.Fatalf("CommitGraphSettings() = %+v, %v", settings, err)
	}
}
