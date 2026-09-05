//go:build oracle

package pack

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func (o *gitOracle) packObjects(args []string, stdin string) []byte {
	o.t.Helper()
	cmd := o.command(o.repo, append([]string{"pack-objects", "--stdout"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git pack-objects returned error %v", err)
	}
	return out
}

func (o *gitOracle) objectIDsFor(revs ...string) []string {
	o.t.Helper()
	var ids []string
	for _, line := range o.lines(o.repo, append([]string{"rev-list", "--objects"}, revs...)...) {
		if fields := strings.Fields(line); len(fields) > 0 {
			ids = append(ids, fields[0])
		}
	}
	if len(ids) == 0 {
		o.t.Fatalf("git rev-list --objects %v listed no objects", revs)
	}
	return ids
}

const thinRevisionWant, thinRevisionHave = "HEAD~9", "HEAD~10"

type gitCatFileResolver struct {
	oracle *gitOracle
}

func (r *gitCatFileResolver) ResolveBase(id hash.ObjectID, depth int) (object.Type, []byte, error) {
	found := r.oracle.catFile([]string{id.String()})
	obj, ok := found[id]
	if !ok {
		return 0, nil, ErrBaseNotFound
	}
	return obj.kind, obj.data, nil
}

func TestOracleIndexPackAcceptsAFullGitPack(t *testing.T) {
	oracle := newGitOracle(t)
	names := oracle.objectNames()
	wanted := oracle.catFile(names)
	packBytes := oracle.packObjects(nil, strings.Join(names, "\n")+"\n")

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), bytes.NewReader(packBytes), dir, IndexOptions{})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}
	if result.Objects != len(names) {
		t.Fatalf("Objects = %d, git listed %d", result.Objects, len(names))
	}

	verifyOut := oracle.run(oracle.repo, "verify-pack", "-v", result.IndexPath)
	verified := 0
	for _, line := range strings.Split(strings.ReplaceAll(verifyOut, "\r\n", "\n"), "\n") {
		if fields := strings.Fields(line); len(fields) >= 5 && len(fields[0]) == hash.HexSize {
			verified++
		}
	}
	if verified != len(names) {
		t.Fatalf("git verify-pack listed %d objects, want %d", verified, len(names))
	}

	store := openStore(t, dir)
	for id, want := range wanted {
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("store.Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != want.kind || !bytes.Equal(data, want.data) {
			t.Errorf("store.Get(%s) did not reproduce the object git holds", id)
		}
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestOracleIndexPackFixesAGitThinPack(t *testing.T) {
	oracle := newGitOracle(t)
	packBytes := oracle.packObjects([]string{"--thin", "--revs"}, thinRevisionWant+"\n^"+thinRevisionHave+"\n")
	wanted := oracle.catFile(oracle.objectIDsFor(thinRevisionWant, "^"+thinRevisionHave))

	dir := t.TempDir()
	result, err := IndexPack(t.Context(), bytes.NewReader(packBytes), dir,
		IndexOptions{Bases: &gitCatFileResolver{oracle: oracle}, FixThin: true})
	if err != nil {
		t.Fatalf("IndexPack returned error %v", err)
	}

	verifyOut := oracle.run(oracle.repo, "verify-pack", "-v", result.IndexPath)
	if !strings.Contains(verifyOut, "\n") {
		t.Fatalf("git verify-pack produced no output:\n%s", verifyOut)
	}

	store := openStore(t, dir)
	for id, want := range wanted {
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("store.Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != want.kind || !bytes.Equal(data, want.data) {
			t.Errorf("store.Get(%s) did not reproduce the object git holds", id)
		}
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestOracleIndexPackRejectsAGitThinPackWithoutFixThin(t *testing.T) {
	oracle := newGitOracle(t)
	packBytes := oracle.packObjects([]string{"--thin", "--revs"}, thinRevisionWant+"\n^"+thinRevisionHave+"\n")
	if !packHoldsAnExternalRefDelta(t, packBytes, oracle.objectIDsFor(thinRevisionWant, "^"+thinRevisionHave)) {
		t.Skip("git did not produce a genuinely thin pack for this revision range")
	}

	dir := t.TempDir()
	if _, err := IndexPack(t.Context(), bytes.NewReader(packBytes), dir, IndexOptions{}); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("IndexPack returned %v, want %v", err, ErrBaseNotFound)
	}
}

func packHoldsAnExternalRefDelta(t *testing.T, packBytes []byte, included []string) bool {
	t.Helper()
	inSet := make(map[string]struct{}, len(included))
	for _, id := range included {
		inSet[id] = struct{}{}
	}
	reader, err := NewReader(bytes.NewReader(packBytes))
	if err != nil {
		t.Fatalf("NewReader returned error %v", err)
	}
	for {
		entry, err := reader.NextObject()
		if err != nil {
			return false
		}
		if entry.Header.Kind == KindRefDelta {
			if _, ok := inSet[entry.Header.BaseID.String()]; !ok {
				return true
			}
		}
	}
}
