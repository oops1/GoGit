package attributes

import (
	"fmt"
	"sync"
	"testing"
)

func concurrentFiles() map[string]string {
	files := map[string]string{
		".gitignore":     "*.log\nbuild/\n!keep.log\n",
		".gitattributes": "* text=auto\n*.png binary\n",
	}
	for dir := range 16 {
		files[fmt.Sprintf("d%02d/.gitignore", dir)] = "*.tmp\n!important.tmp\n"
		files[fmt.Sprintf("d%02d/.gitattributes", dir)] = "*.go text eol=lf\n"
	}
	return files
}

func TestMatcherAnswersTheSameWhenManyGoroutinesAskAtOnce(t *testing.T) {
	files := concurrentFiles()
	shared := testMatcher(files, IgnoreOptions{})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for round := range 200 {
				dir := (round + worker) % 16
				for name, wantIgnored := range map[string]bool{
					fmt.Sprintf("d%02d/a.log", dir):         true,
					fmt.Sprintf("d%02d/keep.log", dir):      false,
					fmt.Sprintf("d%02d/x.tmp", dir):         true,
					fmt.Sprintf("d%02d/important.tmp", dir): false,
					fmt.Sprintf("d%02d/build/o.txt", dir):   true,
					fmt.Sprintf("d%02d/src/main.go", dir):   false,
				} {
					if got, _ := shared.Ignored(name, false); got != wantIgnored {
						t.Errorf("Ignored(%q) = %v, want %v", name, got, wantIgnored)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func TestAttributesAnswerTheSameWhenManyGoroutinesAskAtOnce(t *testing.T) {
	shared := New(AttributeOptions{Work: MemoryLoader(concurrentFiles())})
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for round := range 200 {
				dir := (round + worker) % 16
				path := fmt.Sprintf("d%02d/src/file%d.go", dir, round%7)
				values := shared.Get(path, "text", "eol")
				if !values["text"].IsSet() || values["eol"].Text() != "lf" {
					t.Errorf("Get(%q) = %v, want text set and eol=lf", path, values)
					return
				}
				if !shared.Get(fmt.Sprintf("d%02d/pic.png", dir), "binary")["binary"].IsSet() {
					t.Errorf("pic.png in d%02d is not binary", dir)
					return
				}
			}
		})
	}
	wg.Wait()
}
