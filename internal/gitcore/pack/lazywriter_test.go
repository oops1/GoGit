package pack

import (
	"bytes"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type describingSource struct {
	*fakeSource
	gets     map[hash.ObjectID]int
	infoFail error
}

func newDescribingSource(source *fakeSource) *describingSource {
	return &describingSource{fakeSource: source, gets: map[hash.ObjectID]int{}}
}

func (s *describingSource) Get(id hash.ObjectID) (object.Type, []byte, error) {
	s.gets[id]++
	return s.fakeSource.Get(id)
}

func (s *describingSource) Info(id hash.ObjectID) (object.Type, int64, error) {
	if s.infoFail != nil {
		return 0, 0, s.infoFail
	}
	obj := s.objects[id]
	return obj.kind, int64(len(obj.data)), nil
}

func similarPackSource() (*fakeSource, []hash.ObjectID) {
	source := newFakeSource()
	var ids []hash.ObjectID
	for seed := range 12 {
		ids = append(ids, source.add(object.TypeBlob, similarBlob(seed%9, 60+seed)))
	}
	ids = append(ids, source.add(object.TypeBlob, []byte{}))
	ids = append(ids, source.add(object.TypeCommit, []byte("tree deadbeef\nmessage\n")))
	ids = append(ids, source.add(object.TypeTree, []byte("100644 blob deadbeef\tfile.txt\n")))
	return source, ids
}

func TestWritePackLoadsObjectsOneAtATimeFromASourceThatDescribesThem(t *testing.T) {
	plain, ids := similarPackSource()
	opts := WriteOptions{Window: 4, Depth: 3}
	var eager bytes.Buffer
	if _, err := WritePack(t.Context(), &eager, plain, ids, opts); err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}

	lazy := newDescribingSource(plain)
	var described bytes.Buffer
	result, err := WritePack(t.Context(), &described, lazy, ids, opts)
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if !bytes.Equal(eager.Bytes(), described.Bytes()) {
		t.Fatal("a describing source produced a different pack")
	}
	for _, id := range ids {
		if lazy.gets[id] == 0 || lazy.gets[id] > 2 {
			t.Fatalf("object %s was read %d times", id, lazy.gets[id])
		}
	}
	packfile := openWrittenPack(t, described.Bytes())
	for _, entry := range result.Entries {
		kind, data, err := packfile.ObjectAt(entry.Offset)
		want := plain.objects[entry.ID]
		if err != nil || kind != want.kind || !bytes.Equal(data, want.data) {
			t.Fatalf("object %s read back as (%v, %d bytes, %v)", entry.ID, kind, len(data), err)
		}
	}
}

func TestWritePackReportsADescribingSourceThatFails(t *testing.T) {
	plain, ids := similarPackSource()
	broken := errors.New("info is broken")
	failing := newDescribingSource(plain)
	failing.infoFail = broken
	if _, err := WritePack(t.Context(), &bytes.Buffer{}, failing, ids, WriteOptions{}); !errors.Is(err, broken) {
		t.Fatalf("WritePack returned %v, want %v", err, broken)
	}

	lost := errors.New("object vanished")
	plain.failNextGet(ids[3], lost)
	if _, err := WritePack(t.Context(), &bytes.Buffer{}, newDescribingSource(plain), ids, WriteOptions{}); !errors.Is(err, lost) {
		t.Fatalf("WritePack returned %v, want %v", err, lost)
	}
}
