package remote

import (
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refspec"
)

func putBlob(t *testing.T, db *odb.DB, content string) hash.ObjectID {
	t.Helper()
	id, err := db.Put(object.TypeBlob, []byte(content))
	if err != nil {
		t.Fatalf("db.Put(blob) returned error %v", err)
	}
	return id
}

func putTree(t *testing.T, db *odb.DB, entries ...object.TreeEntry) hash.ObjectID {
	t.Helper()
	tree := &object.Tree{Entries: entries}
	tree.Sort()
	id, err := db.Put(object.TypeTree, tree.Encode())
	if err != nil {
		t.Fatalf("db.Put(tree) returned error %v", err)
	}
	return id
}

func putCommitWithTree(t *testing.T, db *odb.DB, when time.Time, message string, tree hash.ObjectID, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	commit := &object.Commit{
		Tree:      tree,
		Parents:   parents,
		Author:    testSignature(when),
		Committer: testSignature(when),
		Message:   message,
	}
	id, err := db.Put(object.TypeCommit, commit.Encode())
	if err != nil {
		t.Fatalf("db.Put(commit) returned error %v", err)
	}
	return id
}

func putTag(t *testing.T, db *odb.DB, name string, target hash.ObjectID, when time.Time) hash.ObjectID {
	t.Helper()
	tag := &object.Tag{
		Object:     target,
		ObjectType: object.TypeCommit,
		Name:       name,
		Tagger:     &object.Signature{Name: "Test", Email: "test@example.com", When: when},
		Message:    name,
	}
	id, err := db.Put(object.TypeTag, tag.Encode())
	if err != nil {
		t.Fatalf("db.Put(tag) returned error %v", err)
	}
	return id
}

func mustParseSpecs(t *testing.T, texts ...string) []refspec.RefSpec {
	t.Helper()
	specs, err := refspec.ParseAll(texts)
	if err != nil {
		t.Fatalf("refspec.ParseAll returned error %v", err)
	}
	return specs
}

func testWhen() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}
