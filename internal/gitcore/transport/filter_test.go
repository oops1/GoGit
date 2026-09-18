package transport

import (
	"errors"
	"testing"
)

func TestParseFilterAcceptsTheSpecificationsGitSends(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		zero      bool
		blobSize  int64
		blobDepth int
		omitBlob  bool
		treeDepth int
		omitTree  bool
	}{
		{name: "empty", spec: "", zero: true},
		{name: "blobNone", spec: "blob:none", omitBlob: true},
		{name: "blobLimitBytes", spec: "blob:limit=16", blobSize: 16, omitBlob: true},
		{name: "blobUnderLimit", spec: "blob:limit=16", blobSize: 15},
		{name: "blobLimitKilobytes", spec: "blob:limit=1k", blobSize: 1024, omitBlob: true},
		{name: "blobLimitMegabytes", spec: "blob:limit=1M", blobSize: 1 << 20, omitBlob: true},
		{name: "blobLimitGigabytes", spec: "blob:limit=1g", blobSize: 1 << 30, omitBlob: true},
		{name: "treeRoot", spec: "tree:0", omitBlob: true, omitTree: true},
		{name: "treeBelowRoot", spec: "tree:1", blobDepth: 1, omitBlob: true, treeDepth: 1, omitTree: true},
		{name: "treeKeepsRoot", spec: "tree:1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter, err := ParseFilter(tc.spec)
			if err != nil {
				t.Fatalf("ParseFilter returned error %v", err)
			}
			if filter.IsZero() != tc.zero || filter.Spec() != tc.spec {
				t.Fatalf("filter = %+v, want zero %v and spec %q", filter, tc.zero, tc.spec)
			}
			if got := filter.OmitsBlob(tc.blobSize, tc.blobDepth); got != tc.omitBlob {
				t.Fatalf("OmitsBlob(%d, %d) = %v, want %v", tc.blobSize, tc.blobDepth, got, tc.omitBlob)
			}
			if got := filter.OmitsTree(tc.treeDepth); got != tc.omitTree {
				t.Fatalf("OmitsTree(%d) = %v, want %v", tc.treeDepth, got, tc.omitTree)
			}
		})
	}
}

func TestParseFilterRejectsSpecificationsItCannotApply(t *testing.T) {
	for _, spec := range []string{"sparse:oid=HEAD", "blob:limit=", "blob:limit=abc", "blob:limit=-1", "blob:limit=1t", "tree:x", "tree:-1", "object:type=blob"} {
		t.Run(spec, func(t *testing.T) {
			if _, err := ParseFilter(spec); !errors.Is(err, ErrUnsupportedFilter) {
				t.Fatalf("ParseFilter(%q) returned %v, want ErrUnsupportedFilter", spec, err)
			}
		})
	}
}
