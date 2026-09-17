//go:build oracle

package ops

import (
	"bytes"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/linelog"
)

const (
	funcProgramC = `#include <stdio.h>

static int helper(int value)
{
	return value * 2;
}

public:
int main(int argc, char **argv)
{
	int total = helper(argc);
label:
	printf("%d\n", total);
	return 0;
}
`
	funcProgramGo = `package shapes

type Rect struct {
	W, H float64
}

func (r Rect) Area() float64 {
	return r.W * r.H
}

func Scale(r Rect, k float64) Rect {
	return Rect{r.W * k, r.H * k}
}
`
	funcProgramPy = `import math

class Circle:
    def __init__(self, r):
        self.r = r

    def area(self):
        return math.pi * self.r ** 2

def helper(x):
    return x + 1
`
)

func funcLogOf(t *testing.T, o *oracle, dir, arg string) string {
	t.Helper()
	spec, err := linelog.ParseArg(arg)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for entry, err := range LineHistory(t.Context(), o.openRepo(dir), "HEAD", []linelog.Spec{spec}, LineHistoryOptions{}) {
		if err != nil {
			t.Fatalf("LineHistory(%q) returned error %v", arg, err)
		}
		out.WriteString(entry.Commit.String() + "\n")
		if err := (&linelog.Entry{Files: entry.Files}).WritePatch(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}

func TestOracleFunctionLineHistoryFollowsDiffDrivers(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("funcs")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.run(dir, "config", "diff.custom.xfuncname", "^(fn|proc) ")
	o.run(dir, "config", "diff.basic.funcname", "^\\(def\\|class\\) ")
	b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
	rules := "*.c diff=cpp\n*.go diff=golang\n*.py diff=python\ncustom.txt diff=custom\nbasic.txt diff=basic\n"
	custom := "intro\n  fn alpha\n  body a\nfn beta\n  body b\nproc gamma\n  body c\n"
	files := map[string]string{
		".gitattributes": rules, "prog.c": funcProgramC, "shapes.go": funcProgramGo, "shape.py": funcProgramPy,
		"plain.txt": funcProgramPy, "custom.txt": custom, "basic.txt": funcProgramPy,
	}
	b.commit("base", files)
	edit := func(name, from, to string) {
		files[name] = strings.Replace(files[name], from, to, 1)
	}
	edit("prog.c", "value * 2", "value * 3")
	edit("prog.c", "return 0;", "return total;")
	edit("shapes.go", "r.W * r.H\n", "r.H * r.W\n")
	edit("shape.py", "self.r = r", "self.r = float(r)")
	edit("plain.txt", "self.r = r", "self.r = float(r)")
	edit("basic.txt", "return x + 1", "return x + 2")
	edit("custom.txt", "body b", "body B")
	b.commit("edit bodies", map[string]string{"prog.c": files["prog.c"], "shapes.go": files["shapes.go"], "shape.py": files["shape.py"], "plain.txt": files["plain.txt"], "basic.txt": files["basic.txt"], "custom.txt": files["custom.txt"]})
	edit("prog.c", "int total", "long total")
	edit("shapes.go", "Rect{r.W * k", "Rect{W: r.W * k")
	edit("shape.py", "math.pi", "3.14159")
	edit("plain.txt", "math.pi", "3.14159")
	edit("custom.txt", "body a", "body A")
	b.commit("edit more", map[string]string{"prog.c": files["prog.c"], "shapes.go": files["shapes.go"], "shape.py": files["shape.py"], "plain.txt": files["plain.txt"], "custom.txt": files["custom.txt"]})

	for _, arg := range []string{
		":main:prog.c", ":helper:prog.c", ":Area:shapes.go", ":Scale:shapes.go", ":Rect:shapes.go",
		":area:shape.py", ":__init__:shape.py", ":helper:shape.py", "^:Circle:shape.py", ":import:plain.txt", ":class:plain.txt",
		":a:custom.txt", ":beta:custom.txt", ":gamma:custom.txt", ":helper:basic.txt", ":Circle:basic.txt",
	} {
		want := b.git("log", "--format=%H", "--no-color", "-L", arg)
		if got := funcLogOf(t, o, dir, arg); got != want {
			t.Errorf("line history for %q differs\n--- ours\n%s\n--- git\n%s", arg, got, want)
		}
	}
}

func TestOracleLineHistoryMatchesGitLogL(t *testing.T) {
	for _, renames := range []string{"true", "false"} {
		t.Run("diff.renames="+renames, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			newOracleRepo(o, dir)
			o.run(dir, "config", "core.autocrlf", "false")
			o.run(dir, "config", "diff.renames", renames)
			b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
			f := lines("f", 12)
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.commit("edit", map[string]string{"f": editLine(f, 4, "EDITED")})
			b.git("branch", "side")
			b.commit("move", map[string]string{"f": "", "moved": editLine(editLine(f, 4, "EDITED"), 5, "MOVED")})
			b.git("checkout", "-q", "side")
			b.commit("side", map[string]string{"keep": "side\n"})
			b.git("checkout", "-q", "main")
			b.git("merge", "-q", "--no-edit", "side")

			want := strings.Fields(b.git("log", "--format=%H", "-s", "-L", "3,8:moved"))
			var got []string
			for entry, err := range LineHistory(t.Context(), o.openRepo(dir), "HEAD", []linelog.Spec{{Range: "3,8", Path: "moved"}}, LineHistoryOptions{}) {
				if err != nil {
					t.Fatalf("LineHistory returned error %v", err)
				}
				got = append(got, entry.Commit.String())
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("line history = %v, git says %v", got, want)
			}
		})
	}
}
