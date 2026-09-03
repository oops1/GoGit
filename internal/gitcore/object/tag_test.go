package object_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestParseTagReadsHeadersAndMessage(t *testing.T) {
	tag := namedFixture(t, "tag_annotated").tag(t)
	if tag.Object != namedFixture(t, "commit_child").id {
		t.Fatalf("Object = %s", tag.Object)
	}
	if tag.ObjectType != object.TypeCommit || tag.Name != "v1.0" {
		t.Fatalf("type/name = %s/%q", tag.ObjectType, tag.Name)
	}
	if tag.Tagger == nil || tag.Tagger.Name != "C O Mitter" {
		t.Fatalf("Tagger = %+v", tag.Tagger)
	}
	if tag.Message != "annotated tag message\n" {
		t.Fatalf("Message = %q", tag.Message)
	}
}

func TestParseTagAcceptsEveryTargetType(t *testing.T) {
	blobTag := namedFixture(t, "tag_blob").tag(t)
	if blobTag.ObjectType != object.TypeBlob || blobTag.Object != namedFixture(t, "blob_hello").id {
		t.Fatalf("blob tag = %+v", blobTag)
	}
	nested := namedFixture(t, "tag_nested").tag(t)
	if nested.ObjectType != object.TypeTag || nested.Object != namedFixture(t, "tag_annotated").id {
		t.Fatalf("nested tag = %+v", nested)
	}
	if len(nested.Extra) != 1 || nested.Extra[0].Key != "x-note" {
		t.Fatalf("nested tag extra headers = %+v", nested.Extra)
	}
}

func TestParseTagAcceptsAMissingTagger(t *testing.T) {
	tag := namedFixture(t, "tag_no_tagger").tag(t)
	if tag.Tagger != nil {
		t.Fatalf("Tagger = %+v, want nil", tag.Tagger)
	}
	if tag.Name != "v0.0-legacy" {
		t.Fatalf("Name = %q", tag.Name)
	}
}

func TestSplitMessageSeparatesTheSignatureFromTheBody(t *testing.T) {
	tag := namedFixture(t, "tag_signed").tag(t)
	message, signature := tag.SplitMessage()
	if message != "signed tag message\n" {
		t.Fatalf("message = %q", message)
	}
	if !strings.HasPrefix(signature, "-----BEGIN PGP SIGNATURE-----\n") ||
		!strings.HasSuffix(signature, "-----END PGP SIGNATURE-----\n") {
		t.Fatalf("signature = %q", signature)
	}
	if message+signature != tag.Message {
		t.Fatal("SplitMessage lost bytes")
	}
}

func TestSplitMessageReturnsTheWholeBodyWhenUnsigned(t *testing.T) {
	tag := namedFixture(t, "tag_annotated").tag(t)
	message, signature := tag.SplitMessage()
	if message != tag.Message || signature != "" {
		t.Fatalf("message = %q, signature = %q", message, signature)
	}
}

func TestSplitMessageFindsASignatureOnTheLastUnterminatedLine(t *testing.T) {
	tag := &object.Tag{Message: "body\n-----BEGIN SSH SIGNATURE-----"}
	message, signature := tag.SplitMessage()
	if message != "body\n" || signature != "-----BEGIN SSH SIGNATURE-----" {
		t.Fatalf("message = %q, signature = %q", message, signature)
	}
}

func TestSplitMessageIgnoresBannersInsideALine(t *testing.T) {
	tag := &object.Tag{Message: "see -----BEGIN PGP SIGNATURE----- inline\n"}
	message, signature := tag.SplitMessage()
	if message != tag.Message || signature != "" {
		t.Fatalf("message = %q, signature = %q", message, signature)
	}
}

func TestParseTagRejectsBrokenObjects(t *testing.T) {
	const good = "object cd486dc672a4de7c98fdcdbff307b842f84a8c4c\ntype commit\ntag v1\n"
	cases := []struct {
		name string
		data string
		want error
	}{
		{"no blank line", good, object.ErrMalformed},
		{"object is not a hash", "object zz\n\n", object.ErrMalformed},
		{"unknown target type", "object cd486dc672a4de7c98fdcdbff307b842f84a8c4c\ntype delta\n\n", object.ErrUnknownType},
		{"tagger is not an identity", good + "tagger nobody\n\n", object.ErrInvalidSignature},
		{"missing object", "type commit\ntag v1\n\n", object.ErrMissingHeader},
		{"missing type", "object cd486dc672a4de7c98fdcdbff307b842f84a8c4c\ntag v1\n\n", object.ErrMissingHeader},
		{"missing name", "object cd486dc672a4de7c98fdcdbff307b842f84a8c4c\ntype commit\n\n", object.ErrMissingHeader},
		{"duplicate tag header", good + "tag v2\n\n", object.ErrDuplicateHeader},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := object.ParseTag([]byte(c.data)); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}
