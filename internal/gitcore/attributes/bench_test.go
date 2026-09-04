package attributes

import (
	"fmt"
	"strings"
	"testing"
)

const benchmarkPathCount = 10000

func benchmarkFiles() map[string]string {
	files := map[string]string{
		".gitignore": strings.Join([]string{
			"# generated benchmark ignore file",
			"*.log",
			"!keep.log",
			"build/",
			"/rootonly.txt",
			"doc/**/draft.txt",
			"**/tmp",
			"[Cc]ache",
			"a**b.txt",
			"*.o",
			"*.tmp",
		}, "\n") + "\n",
		"info/exclude":  "info-only.txt\n*.excl\n",
		"global/ignore": "global-only.txt\n*.glb\n",
	}
	for dir := range 20 {
		files[fmt.Sprintf("dir%02d/.gitignore", dir)] = fmt.Sprintf("local%02d.txt\n*.bak\n!keep.bak\n", dir)
	}
	return files
}

func benchmarkPaths() []Path {
	paths := make([]Path, 0, benchmarkPathCount)
	suffixes := []string{".log", ".txt", ".o", ".tmp", ".bak", ".excl", ".glb"}
	for i := range benchmarkPathCount {
		dir := fmt.Sprintf("dir%02d/nested%d", i%20, i%7)
		name := fmt.Sprintf("file%05d%s", i, suffixes[i%len(suffixes)])
		paths = append(paths, Path{Name: dir + "/" + name})
	}
	return paths
}

func BenchmarkIgnored(b *testing.B) {
	paths := benchmarkPaths()
	if len(paths) != benchmarkPathCount {
		b.Fatalf("benchmark uses %d paths, want %d", len(paths), benchmarkPathCount)
	}
	files := benchmarkFiles()
	for b.Loop() {
		matcher := NewMatcher(IgnoreOptions{
			Work:         MemoryLoader(files),
			Global:       MemoryLoader(files),
			InfoExclude:  "info/exclude",
			ExcludesFile: "global/ignore",
		})
		for _, path := range paths {
			matcher.Ignored(path.Name, path.IsDir)
		}
	}
}

func BenchmarkCheck(b *testing.B) {
	paths := benchmarkPaths()
	files := benchmarkFiles()
	for b.Loop() {
		matcher := NewMatcher(IgnoreOptions{Work: MemoryLoader(files), Global: MemoryLoader(files)})
		for match, err := range matcher.Check(paths) {
			if err != nil {
				b.Fatalf("Check(%q) returned error %v", match.Path, err)
			}
		}
	}
}

func BenchmarkAttributesGet(b *testing.B) {
	paths := benchmarkPaths()
	files := map[string]string{
		".gitattributes": "* text=auto\n*.o binary\n*.txt diff=text\n[attr]mymacro -text diff\n",
	}
	for dir := range 20 {
		files[fmt.Sprintf("dir%02d/.gitattributes", dir)] = "*.bak mymacro\n*.tmp -diff\n"
	}
	for b.Loop() {
		attrs := New(AttributeOptions{Work: MemoryLoader(files)})
		for _, path := range paths {
			attrs.Get(path.Name, "text", "diff", "merge")
		}
	}
}
