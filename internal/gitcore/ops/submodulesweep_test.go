package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/repo"
)

type faultCounter struct {
	calls atomic.Int64
	at    int64
}

func (c *faultCounter) fires() bool {
	return c.calls.Add(1) == c.at
}

func (c *faultCounter) reached() bool {
	return c.calls.Load() >= c.at
}

func injectFault[F any](t *testing.T, seam *F, counter *faultCounter) {
	t.Helper()
	original := reflect.ValueOf(*seam)
	kind := original.Type()
	fault := errSubmoduleFault
	wrapped := reflect.MakeFunc(kind, func(args []reflect.Value) []reflect.Value {
		if !counter.fires() {
			if kind.IsVariadic() {
				return original.CallSlice(args)
			}
			return original.Call(args)
		}
		out := make([]reflect.Value, kind.NumOut())
		for i := range out {
			out[i] = reflect.Zero(kind.Out(i))
		}
		out[len(out)-1] = reflect.ValueOf(&fault).Elem()
		return out
	})
	replaceSeam(t, seam, wrapped.Interface().(F))
}

type faultPoint struct {
	name    string
	install func(t *testing.T, counter *faultCounter)
}

type faultContext struct {
	context.Context
	counter *faultCounter
}

func (c faultContext) Err() error {
	if c.counter.calls.Add(1) >= c.counter.at {
		return context.Canceled
	}
	return c.Context.Err()
}

func seamFault[F any](name string, seam *F) faultPoint {
	return faultPoint{name: name, install: func(t *testing.T, counter *faultCounter) { injectFault(t, seam, counter) }}
}

var submoduleFaultPoints = []faultPoint{
	seamFault("odb", &odbOpen),
	seamFault("refs", &refsOpen),
	seamFault("refs lookup", &refsLookup),
	seamFault("get", &dbGet),
	seamFault("tree", &dbTree),
	seamFault("commit", &dbCommit),
	seamFault("put", &dbPut),
	seamFault("tx commit", &txCommit),
	seamFault("tx set", &txSet),
	seamFault("tx symbolic", &txSetSymbolic),
	seamFault("tx detach", &txDetach),
	seamFault("index write", &idxWrite),
	seamFault("open root", &fsOpenRoot),
	seamFault("root open", &fsRootOpen),
	seamFault("root open file", &fsRootOpenFile),
	seamFault("root lstat", &fsRootLstat),
	seamFault("root read", &fsRootReadFile),
	seamFault("root remove", &fsRootRemove),
	seamFault("root rename", &fsRootRename),
	seamFault("root mkdir", &fsRootMkdir),
	seamFault("root write", &fsRootWriteFile),
	seamFault("file close", &fsFileClose),
	seamFault("hash", &hashSum),
	seamFault("read index", &submoduleReadIndex),
	seamFault("load modules", &submoduleLoad),
	seamFault("write config", &writeSubmoduleConfig),
	seamFault("edit config", &editSubmoduleConfig),
	seamFault("rename git dir", &renameGitDir),
	seamFault("remove tree", &removeSubmoduleTree),
	seamFault("validate path", &validateSubmodulePath),
	seamFault("worktree", &worktreeOpen),
	seamFault("clone init", &cloneRepoInit),
	seamFault("clone layout", &cloneRepoOpenLayout),
	seamFault("read dir", &readSubmoduleDir),
	seamFault("open submodule", &submoduleOpen),
	seamFault("open submodule layout", &submoduleOpenLayout),
	seamFault("read index file", &readIndexFile),
}

type sweepWorld struct {
	root   string
	global string
}

func (w sweepWorld) open(t *testing.T, rel string) *repo.Repository {
	t.Helper()
	r, err := repo.Open(filepath.Join(w.root, filepath.FromSlash(rel)), repo.OpenOptions{NoSystem: true, GlobalFile: w.global})
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func copyWorld(t *testing.T, template *submoduleWorld) sweepWorld {
	t.Helper()
	root := filepath.Join(t.TempDir(), "w")
	if err := os.CopyFS(root, os.DirFS(template.root)); err != nil {
		t.Fatalf("CopyFS returned error %v", err)
	}
	return sweepWorld{root: root, global: template.global}
}

func sweepSubmoduleFaults(t *testing.T, template *submoduleWorld, run func(ctx context.Context, t *testing.T, w sweepWorld) error) {
	t.Helper()
	w := copyWorld(t, template)
	if err := run(t.Context(), t, w); err != nil {
		t.Fatalf("the scenario fails without faults: %v", err)
	}
	for at := int64(1); ; at++ {
		counter := &faultCounter{at: at}
		reached := false
		t.Run(fmt.Sprintf("context %d", at), func(t *testing.T) {
			w := copyWorld(t, template)
			_ = run(faultContext{Context: t.Context(), counter: counter}, t, w)
			reached = counter.reached()
		})
		if !reached {
			break
		}
	}
	for _, point := range submoduleFaultPoints {
		for at := int64(1); ; at++ {
			counter := &faultCounter{at: at}
			reached := false
			t.Run(fmt.Sprintf("%s %d", point.name, at), func(t *testing.T) {
				w := copyWorld(t, template)
				point.install(t, counter)
				_ = run(t.Context(), t, w)
				reached = counter.reached()
			})
			if !reached {
				break
			}
		}
	}
}
