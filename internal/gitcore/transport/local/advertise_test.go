package local

import (
	"context"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func dialSession(t testing.TB, dir string) transport.Session {
	t.Helper()
	sess, err := Dial(t.Context(), dir, transport.UploadPack, transport.Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func findRef(refs []transport.Ref, name string) (transport.Ref, bool) {
	for _, ref := range refs {
		if ref.Name == name {
			return ref, true
		}
	}
	return transport.Ref{}, false
}

func TestAdvertiseOnEmptyRepositoryHasNoRefs(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	adv, err := sess.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Head != "refs/heads/main" {
		t.Fatalf("adv.Head = %q, want refs/heads/main", adv.Head)
	}
	if len(adv.Refs) != 0 {
		t.Fatalf("adv.Refs = %v, want none", adv.Refs)
	}
}

func TestAdvertiseListsHeadBranchesAndTags(t *testing.T) {
	src := newTestRepo(t, true)
	first := src.commit("main", map[string]string{"a.txt": "a"})
	src.lightweightTag("light", first)
	tagID := src.annotatedTag("annotated", first, object.TypeCommit)

	sess := dialSession(t, src.dir)
	adv, err := sess.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Head != "refs/heads/main" {
		t.Fatalf("adv.Head = %q, want refs/heads/main", adv.Head)
	}
	head, ok := findRef(adv.Refs, "HEAD")
	if !ok {
		t.Fatal("advertisement is missing HEAD")
	}
	if head.ID != first || head.Symref != "refs/heads/main" {
		t.Fatalf("HEAD ref = %+v, want id %s and symref refs/heads/main", head, first)
	}
	branch, ok := findRef(adv.Refs, "refs/heads/main")
	if !ok || branch.ID != first {
		t.Fatalf("refs/heads/main = %+v, want id %s", branch, first)
	}
	light, ok := findRef(adv.Refs, "refs/tags/light")
	if !ok || light.ID != first || !light.Peeled.IsZero() {
		t.Fatalf("refs/tags/light = %+v, want lightweight tag at %s", light, first)
	}
	annotated, ok := findRef(adv.Refs, "refs/tags/annotated")
	if !ok || annotated.ID != tagID || annotated.Peeled != first {
		t.Fatalf("refs/tags/annotated = %+v, want id %s peeled to %s", annotated, tagID, first)
	}
}

func TestAdvertiseWithDetachedHead(t *testing.T) {
	src := newTestRepo(t, true)
	commit := src.commit("main", map[string]string{"a.txt": "hello"})
	src.writeRawHead(commit.String() + "\n")

	sess := dialSession(t, src.dir)
	adv, err := sess.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Head != "" {
		t.Fatalf("adv.Head = %q, want empty for a detached HEAD", adv.Head)
	}
	head, ok := findRef(adv.Refs, "HEAD")
	if !ok || head.ID != commit || head.Symref != "" {
		t.Fatalf("HEAD ref = %+v, want a detached id %s", head, commit)
	}
}

func TestAdvertiseWhenHeadFileIsMissing(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	src.removeHead()

	adv, err := sess.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Head != "" {
		t.Fatalf("adv.Head = %q, want empty when HEAD is missing", adv.Head)
	}
	if _, ok := findRef(adv.Refs, "HEAD"); ok {
		t.Fatal("advertisement carries a HEAD entry although HEAD is missing")
	}
}

func TestAdvertiseFailsWhenHeadFileIsMalformed(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	src.writeRawHead("not a valid head\n")

	if _, err := sess.Advertise(t.Context()); err == nil {
		t.Fatal("Advertise accepted a malformed HEAD file")
	}
}

func TestAdvertiseFailsWhenHeadTargetIsMalformed(t *testing.T) {
	src := newTestRepo(t, true)
	src.writeRawRef("refs/heads/main", "not a valid ref\n")

	sess := dialSession(t, src.dir)
	if _, err := sess.Advertise(t.Context()); err == nil {
		t.Fatal("Advertise accepted a malformed HEAD target")
	}
}

func TestAdvertiseFailsWhenARefFileIsMalformed(t *testing.T) {
	src := newTestRepo(t, true)
	src.writeRawRef("refs/heads/broken", "not a valid ref\n")

	sess := dialSession(t, src.dir)
	if _, err := sess.Advertise(t.Context()); err == nil {
		t.Fatal("Advertise accepted a malformed reference file")
	}
}

func TestAdvertiseRespectsCanceledContext(t *testing.T) {
	src := newTestRepo(t, true)
	sess := dialSession(t, src.dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sess.Advertise(ctx); err == nil {
		t.Fatal("Advertise on a canceled context returned no error")
	}
}
