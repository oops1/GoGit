package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureSeparator = "=== "

type fixtureSection struct {
	header string
	body   string
}

func loadFixture(t *testing.T, path string) []fixtureSection {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", path, err)
	}
	var sections []fixtureSection
	var body strings.Builder
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, fixtureSeparator) {
			body.WriteString(line)
			continue
		}
		if len(sections) > 0 {
			sections[len(sections)-1].body = body.String()
		}
		body.Reset()
		sections = append(sections, fixtureSection{header: strings.TrimSuffix(line[len(fixtureSeparator):], "\n")})
	}
	if len(sections) > 0 {
		sections[len(sections)-1].body = body.String()
	}
	return sections
}

func readPairFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pairs", name))
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", name, err)
	}
	return string(data)
}

func TestFixturePairsMatchTheCorpus(t *testing.T) {
	for _, pair := range corpus() {
		if got := readPairFile(t, pair.name+".old"); got != pair.old {
			t.Errorf("%s.old in testdata differs from the corpus", pair.name)
		}
		if got := readPairFile(t, pair.name+".new"); got != pair.new {
			t.Errorf("%s.new in testdata differs from the corpus", pair.name)
		}
	}
}

func TestBlobDiffMatchesFixtures(t *testing.T) {
	for _, v := range variants() {
		sections := loadFixture(t, filepath.Join("testdata", "blobs", v.name+".diff"))
		if len(sections) != len(corpus()) {
			t.Fatalf("%s holds %d sections instead of %d", v.name, len(sections), len(corpus()))
		}
		for _, section := range sections {
			t.Run(section.header+"/"+v.name, func(t *testing.T) {
				pair := corpusPair{
					name: section.header,
					old:  readPairFile(t, section.header+".old"),
					new:  readPairFile(t, section.header+".new"),
				}
				if got := renderPair(t, pair, v); got != section.body {
					t.Errorf("output differs from the fixture\n--- want ---\n%s\n--- got ---\n%s", section.body, got)
				}
			})
		}
	}
}

func TestTreeDiffMatchesFixtures(t *testing.T) {
	byName := make(map[string]treePair)
	for _, pair := range treeCorpus() {
		byName[pair.name] = pair
	}
	for _, v := range treeVariants() {
		sections := loadFixture(t, filepath.Join("testdata", "trees", v.name+".diff"))
		if len(sections) != len(byName) {
			t.Fatalf("%s holds %d sections instead of %d", v.name, len(sections), len(byName))
		}
		for _, section := range sections {
			fields := strings.Fields(section.header)
			if len(fields) != 3 {
				t.Fatalf("unusable fixture header %q", section.header)
			}
			t.Run(fields[0]+"/"+v.name, func(t *testing.T) {
				pair, known := byName[fields[0]]
				if !known {
					t.Fatalf("the corpus has no tree pair %q", fields[0])
				}
				store, oldTree, newTree := storeTreePair(t, pair)
				if oldTree.String() != fields[1] || newTree.String() != fields[2] {
					t.Fatalf("tree ids %s %s differ from the fixture %s %s", oldTree, newTree, fields[1], fields[2])
				}
				files, err := Trees(t.Context(), store, oldTree, newTree, v.opts)
				if err != nil {
					t.Fatalf("Trees returned error %v", err)
				}
				if got := renderFiles(t, files, v.kind, v.opts); got != section.body {
					t.Errorf("output differs from the fixture\n--- want ---\n%s\n--- got ---\n%s", section.body, got)
				}
			})
		}
	}
}
