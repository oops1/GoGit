//go:build oracle

package ops

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

const driverSamplesDir = "testdata/diffdrivers"

type lineChange func(at int) bool

var driverChanges = map[string]lineChange{
	"even":   func(at int) bool { return at%2 == 0 },
	"odd":    func(at int) bool { return at%2 == 1 },
	"sparse": func(at int) bool { return at%9 == 8 },
}

func driverSamples(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(driverSamplesDir)
	if err != nil {
		t.Fatal(err)
	}
	samples := map[string]string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(driverSamplesDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		samples[entry.Name()] = string(data)
	}
	return samples
}

func changedLines(text string, change lineChange) string {
	lines := strings.SplitAfter(text, "\n")
	for at, line := range lines {
		if line == "" || !change(at) {
			continue
		}
		body, ending := line, ""
		for _, suffix := range []string{"\r\n", "\n"} {
			if cut, found := strings.CutSuffix(line, suffix); found {
				body, ending = cut, suffix
				break
			}
		}
		lines[at] = body + "~" + ending
	}
	return strings.Join(lines, "")
}

func driverHistory(t *testing.T, o *oracle, dir string, rules string, samples map[string]string) {
	t.Helper()
	o.write(dir, ".gitattributes", rules)
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range samples {
			if !yield(name) {
				return
			}
		}
	})
	for _, name := range names {
		for variant := range driverChanges {
			o.write(dir, name+"-"+variant+".txt", samples[name])
		}
	}
	o.run(dir, "add", "-A")
	o.run(dir, "commit", "-q", "-m", "samples")
	for _, name := range names {
		for variant, change := range driverChanges {
			o.write(dir, name+"-"+variant+".txt", changedLines(samples[name], change))
		}
	}
	o.run(dir, "add", "-A")
	o.run(dir, "commit", "-q", "-m", "edits")
}

func compareHunkHeadersWithGit(t *testing.T, o *oracle, dir string) {
	t.Helper()
	r := o.openRepo(dir)
	for _, context := range []int{0, 1, 3} {
		opts := diff.Defaults()
		opts.Context = context
		details, err := Details(t.Context(), r, "HEAD", DetailsOptions{Diff: opts})
		if err != nil {
			t.Fatalf("Details returned error %v", err)
		}
		var got bytes.Buffer
		for _, file := range details.Changes {
			if err := diff.Unified(&got, file, opts); err != nil {
				t.Fatal(err)
			}
		}
		unified := "-U" + strconv.Itoa(context)
		sameText(t, "git diff "+unified, got.String(), o.run(dir, "diff", unified, "HEAD~1", "HEAD"))
		sameText(t, "git log -p "+unified, got.String(), strings.TrimPrefix(o.run(dir, "log", "-p", unified, "-1", "--format=", "HEAD"), "\n"))
	}
}

func sameText(t *testing.T, what, ours, git string) {
	t.Helper()
	if ours == git {
		return
	}
	oursLines, gitLines := strings.Split(ours, "\n"), strings.Split(git, "\n")
	for at := range min(len(oursLines), len(gitLines)) {
		if oursLines[at] != gitLines[at] {
			t.Errorf("%s differs at line %d:\nours %q\ngit  %q", what, at+1, oursLines[at], gitLines[at])
			return
		}
	}
	t.Errorf("%s differs in length: ours %d lines, git %d lines", what, len(oursLines), len(gitLines))
}

func gitTakesTheWholeLineForBashFunctions(o *oracle) bool {
	dir := o.repoDir("bash-driver-probe")
	newOracleRepo(o, dir)
	o.write(dir, ".gitattributes", "probe.sh diff=bash\n")
	body := "greet() {\n\techo one\n\techo two\n\techo three\n\techo four\n}\n"
	o.write(dir, "probe.sh", body)
	o.run(dir, "add", "-A")
	o.run(dir, "commit", "-q", "-m", "probe")
	o.write(dir, "probe.sh", strings.Replace(body, "echo four", "echo FOUR", 1))
	return strings.Contains(o.run(dir, "diff", "-U0"), "@@ greet() {")
}

func TestOracleHunkHeadersFollowBuiltinDiffDrivers(t *testing.T) {
	o := newOracle(t)
	if !gitTakesTheWholeLineForBashFunctions(o) {
		t.Skip("the installed git predates the userdiff patterns we carry and trims function headers")
	}
	dir := o.repoDir("drivers")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	samples := driverSamples(t)
	var rules strings.Builder
	var mixed strings.Builder
	for name, text := range samples {
		rules.WriteString(name + "-* diff=" + name + "\n")
		mixed.WriteString(text)
	}
	rules.WriteString("set-* diff\n")
	samples["plain"] = mixed.String()
	samples["set"] = mixed.String()
	driverHistory(t, o, dir, rules.String(), samples)
	compareHunkHeadersWithGit(t, o, dir)
}

func TestOracleHunkHeadersFollowConfiguredDiffDrivers(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("configured")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.run(dir, "config", "diff.basic.funcname", "!^ignore\n^\\(def\\|class\\) [a-z]*\n^[A-Z].*:$")
	o.run(dir, "config", "diff.extended.xfuncname", "^ *(proc|fn)( [A-Za-z]+)")
	o.run(dir, "config", "diff.python.xfuncname", "^(class .*)")
	o.run(dir, "config", "diff.default.xfuncname", "^== (.*) ==")
	o.run(dir, "config", "diff.later.funcname", "^def")
	o.run(dir, "config", "diff.later.xfuncname", "^(class|def)")
	o.run(dir, "config", "diff.CamelCase.xfuncname", "^(fn [a-z]+)")
	long := "def " + strings.Repeat("very_long_name_", 8) + "(self):   \r\n"
	custom := "== Intro ==\nignore this line\ndef alpha\n    body one\nclass beta\n    body two\nNotes:\n" +
		"    body three\nproc Gamma\nfn delta\n  fn Epsilon\n" + long + "    body four\r\n" +
		"CR ends here  \r\ntrailing\t\v\f\nfn tail\n    body five\n"
	for index := range 6 {
		custom += "    filler " + strconv.Itoa(index) + "\n"
	}
	samples := map[string]string{
		"basic": custom, "extended": custom, "python": custom, "later": custom,
		"unknown": custom, "plain": custom, "camel": custom, "lower": custom,
	}
	rules := "basic-* diff=basic\nextended-* diff=extended\npython-* diff=python\nlater-* diff=later\n" +
		"unknown-* diff=nosuch\ncamel-* diff=CamelCase\nlower-* diff=camelcase\n"
	driverHistory(t, o, dir, rules, samples)
	compareHunkHeadersWithGit(t, o, dir)
	o.run(dir, "mv", "python-even.txt", "moved-even.txt")
	o.run(dir, "mv", "plain-odd.txt", "moved-odd.py")
	o.write(dir, "moved-even.txt", changedLines(custom, driverChanges["sparse"]))
	o.write(dir, "moved-odd.py", changedLines(custom, driverChanges["sparse"]))
	o.write(dir, ".gitattributes", rules+"*.py diff=python\n")
	o.run(dir, "add", "-A")
	o.run(dir, "commit", "-q", "-m", "renames")
	compareHunkHeadersWithGit(t, o, dir)
}
