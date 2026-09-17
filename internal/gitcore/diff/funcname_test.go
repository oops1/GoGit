package diff

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/posixre"
	"github.com/oops1/gogit/internal/gitcore/userdiff"
)

func pythonMatcher(t *testing.T) *userdiff.Matcher {
	t.Helper()
	driver, _ := userdiff.Builtin("python")
	matcher, err := userdiff.Compile(driver.FuncName)
	if err != nil {
		t.Fatal(err)
	}
	return matcher
}

const pythonOld = "import os\n\nclass Shape:\n    def area(self):\n        a = 1\n        b = 2\n        c = 3\n        d = 4\n        return a\n"

func TestBlobsTakeHunkHeadersFromTheGivenMatcher(t *testing.T) {
	updated := []byte(pythonOld[:len(pythonOld)-len("return a\n")] + "return b\n")
	plain := Blobs([]byte(pythonOld), updated, Defaults())
	if len(plain) != 1 || plain[0].Header != "class Shape:" {
		t.Fatalf("the default rule gave %+v", plain)
	}
	opts := Defaults()
	opts.FuncMatcher = pythonMatcher(t)
	hunks := Blobs([]byte(pythonOld), updated, opts)
	if len(hunks) != 1 || hunks[0].Header != "def area(self):" {
		t.Fatalf("the python driver gave %+v", hunks)
	}
}

func TestTreesAskForTheOldPathDriverBeforeTheNewOne(t *testing.T) {
	store := newMemoryStore()
	updated := pythonOld[:len(pythonOld)-len("return a\n")] + "return b\n"
	old := treeOf(t, store, treeFiles{"shape.py": blobSpec(pythonOld), "shape.txt": blobSpec(pythonOld)})
	next := treeOf(t, store, treeFiles{"shape.py": blobSpec(updated), "shape.txt": blobSpec(updated)})
	python := pythonMatcher(t)
	custom, err := userdiff.Compile(userdiff.Pattern{Source: "^class (.*):$", Flags: posixre.Extended})
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	opts := Defaults()
	opts.FuncMatcher = custom
	opts.FuncNames = func(path string) *userdiff.Matcher {
		asked = append(asked, path)
		if path == "shape.py" {
			return python
		}
		return nil
	}
	files, err := Trees(t.Context(), store, old, next, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Hunks[0].Header != "def area(self):" || files[1].Hunks[0].Header != "class Shape:" {
		t.Fatalf("Trees gave %+v", files)
	}
	if len(asked) != 3 || asked[0] != "shape.py" || asked[1] != "shape.txt" || asked[2] != "shape.txt" {
		t.Fatalf("the drivers were asked for %v", asked)
	}
	opts.FuncNames = nil
	files, err = Trees(t.Context(), store, old, next, opts)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Hunks[0].Header != "Shape" {
		t.Fatalf("an explicit matcher gave %+v", files[0].Hunks)
	}
}
