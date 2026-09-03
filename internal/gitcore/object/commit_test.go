package object_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestParseCommitReadsIdentitiesAndMessage(t *testing.T) {
	commit := namedFixture(t, "commit_root").commit(t)
	if commit.Tree.String() != "346fab697709432f6ecc37fc6630c2e18d281399" {
		t.Fatalf("Tree = %s", commit.Tree)
	}
	if len(commit.Parents) != 0 {
		t.Fatalf("root commit has %d parents", len(commit.Parents))
	}
	if commit.Author.Name != "A U Thor" || commit.Author.Email != "author@example.com" {
		t.Fatalf("author = %+v", commit.Author)
	}
	if commit.Committer.Name != "C O Mitter" || commit.Committer.When.Unix() != 1700000100 {
		t.Fatalf("committer = %+v", commit.Committer)
	}
	if commit.Message != "initial commit\n" {
		t.Fatalf("Message = %q", commit.Message)
	}
	if commit.Encoding != "" || commit.GPGSignature != "" || len(commit.Extra) != 0 {
		t.Fatalf("unexpected optional headers on a plain commit: %+v", commit)
	}
}

func TestParseCommitKeepsEveryParentInOrder(t *testing.T) {
	merge := namedFixture(t, "commit_merge").commit(t)
	if len(merge.Parents) != 3 {
		t.Fatalf("octopus merge has %d parents, want 3", len(merge.Parents))
	}
	want := []string{
		namedFixture(t, "commit_child").id.String(),
		namedFixture(t, "commit_side").id.String(),
		namedFixture(t, "commit_root").id.String(),
	}
	for index, parent := range merge.Parents {
		if parent.String() != want[index] {
			t.Fatalf("parent %d = %s, want %s", index, parent, want[index])
		}
	}
}

func TestParseCommitAcceptsAnEmptyMessage(t *testing.T) {
	commit := namedFixture(t, "commit_empty_message").commit(t)
	if commit.Message != "" {
		t.Fatalf("Message = %q, want empty", commit.Message)
	}
	if !strings.HasSuffix(string(commit.Encode()), "\n\n") {
		t.Fatalf("encoded commit does not end with the header separator: %q", commit.Encode())
	}
}

func TestParseCommitKeepsTheEncodingHeader(t *testing.T) {
	commit := namedFixture(t, "commit_encoding").commit(t)
	if commit.Encoding != "ISO-8859-1" {
		t.Fatalf("Encoding = %q", commit.Encoding)
	}
}

func TestParseCommitUnfoldsTheGPGSignature(t *testing.T) {
	commit := namedFixture(t, "commit_gpgsig").commit(t)
	lines := strings.Split(commit.GPGSignature, "\n")
	if lines[0] != "-----BEGIN PGP SIGNATURE-----" {
		t.Fatalf("signature starts with %q", lines[0])
	}
	if lines[1] != "" {
		t.Fatalf("the blank armour line was not unfolded: %q", lines[1])
	}
	if lines[len(lines)-1] != "-----END PGP SIGNATURE-----" {
		t.Fatalf("signature ends with %q", lines[len(lines)-1])
	}
	if commit.Committer.When.Unix() != 1700000100 {
		t.Fatalf("committer time = %d", commit.Committer.When.Unix())
	}
	if name, _ := commit.Committer.When.Zone(); name != "-0430" {
		t.Fatalf("committer zone = %q", name)
	}
}

func TestParseCommitKeepsUnknownHeaders(t *testing.T) {
	commit := namedFixture(t, "commit_extra_headers").commit(t)
	if len(commit.Extra) != 1 {
		t.Fatalf("Extra = %+v", commit.Extra)
	}
	if commit.Extra[0].Key != "x-custom" {
		t.Fatalf("unknown header key = %q", commit.Extra[0].Key)
	}
	if commit.Extra[0].Value != "first value\ncontinued line of the custom header" {
		t.Fatalf("unknown header value = %q", commit.Extra[0].Value)
	}
	if commit.Encoding != "ISO-8859-1" {
		t.Fatalf("Encoding = %q", commit.Encoding)
	}
	if !strings.HasPrefix(commit.GPGSignature, "-----BEGIN SSH SIGNATURE-----") {
		t.Fatalf("GPGSignature = %q", commit.GPGSignature)
	}
}

func TestParseCommitAcceptsAnAuthorWithoutTimezone(t *testing.T) {
	commit := namedFixture(t, "commit_no_zone").commit(t)
	if !commit.Author.OmitZone || !commit.Committer.OmitZone {
		t.Fatalf("OmitZone = %v/%v", commit.Author.OmitZone, commit.Committer.OmitZone)
	}
	if commit.Author.When.Unix() != 1700000000 {
		t.Fatalf("author time = %d", commit.Author.When.Unix())
	}
}

func TestParseCommitAcceptsUnusualTimezones(t *testing.T) {
	commit := namedFixture(t, "commit_negative_zero_zone").commit(t)
	if name, offset := commit.Author.When.Zone(); name != "-0000" || offset != 0 {
		t.Fatalf("author zone = %q, %d", name, offset)
	}
	if name, offset := commit.Committer.When.Zone(); name != "+1345" || offset != 13*3600+45*60 {
		t.Fatalf("committer zone = %q, %d", name, offset)
	}
	if commit.Committer.Name != "" {
		t.Fatalf("committer name = %q, want empty", commit.Committer.Name)
	}
	if commit.Message != "unusual zones" {
		t.Fatalf("Message = %q", commit.Message)
	}
}

func TestParseCommitRejectsBrokenObjects(t *testing.T) {
	const good = "tree 346fab697709432f6ecc37fc6630c2e18d281399\n" +
		"author A U Thor <author@example.com> 1700000000 +0300\n" +
		"committer C O Mitter <committer@example.com> 1700000100 +0300\n"
	cases := []struct {
		name string
		data string
		want error
	}{
		{"no blank line", good, object.ErrMalformed},
		{"continuation before any header", " orphan\n\n", object.ErrMalformed},
		{"header without a value", "tree\n\n", object.ErrMalformed},
		{"empty header line", "\n\n", object.ErrMalformed},
		{"tree is not a hash", "tree zz\n\n", object.ErrMalformed},
		{"parent is not a hash", good + "parent zz\n\n", object.ErrMalformed},
		{"author is not an identity", "tree 346fab697709432f6ecc37fc6630c2e18d281399\nauthor nobody\n\n", object.ErrInvalidSignature},
		{"committer is not an identity", "tree 346fab697709432f6ecc37fc6630c2e18d281399\nauthor A U Thor <a@example.com> 1 +0000\ncommitter nobody\n\n", object.ErrInvalidSignature},
		{"missing tree", "author A U Thor <a@example.com> 1 +0000\n\n", object.ErrMissingHeader},
		{"missing author", "tree 346fab697709432f6ecc37fc6630c2e18d281399\n\n", object.ErrMissingHeader},
		{"duplicate tree", good + "tree 346fab697709432f6ecc37fc6630c2e18d281399\n\n", object.ErrDuplicateHeader},
		{"duplicate encoding", good + "encoding utf-8\nencoding utf-8\n\n", object.ErrDuplicateHeader},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := object.ParseCommit([]byte(c.data)); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestEncodeCommitPlacesHeadersInGitOrder(t *testing.T) {
	commit := namedFixture(t, "commit_extra_headers").commit(t)
	headers, _, _ := strings.Cut(string(commit.Encode()), "\n\n")
	var keys []string
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(line, " ") {
			continue
		}
		key, _, _ := strings.Cut(line, " ")
		keys = append(keys, key)
	}
	want := []string{"tree", "parent", "author", "committer", "encoding", "x-custom", "gpgsig"}
	if !slices.Equal(keys, want) {
		t.Fatalf("header order = %q, want %q", keys, want)
	}
}
