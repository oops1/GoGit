package attributes

import "testing"

type sparseCase struct {
	path  string
	isDir bool
	want  bool
}

func requireSparse(t *testing.T, sparse *Sparse, tests []sparseCase) {
	t.Helper()
	for _, tc := range tests {
		if got := sparse.Includes(tc.path, tc.isDir); got != tc.want {
			t.Errorf("Includes(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
		}
	}
}

func TestSparsePatternsInheritTheDecisionOfTheirDirectory(t *testing.T) {
	sparse := ParseSparse([]byte("*.txt\n!sub/\nb/sub/\n"), SparseOptions{})
	if sparse.Cone() {
		t.Fatal("patterns without cone mode report cone matching")
	}
	requireSparse(t, sparse, []sparseCase{
		{"a/x.txt", false, true},
		{"a/y.md", false, false},
		{"a/sub/z.txt", false, true},
		{"a/sub/z.md", false, false},
		{"b/sub/z.md", false, true},
		{"b/k.md", false, false},
		{"top.md", false, false},
		{"b/sub", true, true},
		{"a/sub", true, false},
	})
}

func TestSparsePatternsReadCommentsBlankLinesAndCase(t *testing.T) {
	data := []byte("\xef\xbb\xbf# only a\r\n\n/A/  \r\n")
	requireSparse(t, ParseSparse(data, SparseOptions{IgnoreCase: true}), []sparseCase{
		{"a/x", false, true},
		{"b/x", false, false},
		{"#", false, false},
	})
	requireSparse(t, ParseSparse(data, SparseOptions{}), []sparseCase{{"a/x", false, false}, {"A/x", false, true}})
	requireSparse(t, ParseSparse(nil, SparseOptions{}), []sparseCase{{"a", false, false}})
}

func TestSparseConeIncludesRootFilesRecursiveAndParentDirectories(t *testing.T) {
	sparse := ParseSparse([]byte("/*\n!/*/\n/a/\n!/a/*/\n/a/b/\n"), SparseOptions{Cone: true})
	if !sparse.Cone() {
		t.Fatal("cone patterns disabled cone matching")
	}
	requireSparse(t, sparse, []sparseCase{
		{"top", false, true},
		{"c", true, true},
		{"a/x", false, true},
		{"a/c/y", false, false},
		{"a/b/y", false, true},
		{"a/b/deep/z", false, true},
		{"c/z", false, false},
		{"c/d/z", false, false},
	})
}

func TestSparseConeFollowsFullConeAndCase(t *testing.T) {
	requireSparse(t, ParseSparse([]byte("/*\n"), SparseOptions{Cone: true}), []sparseCase{{"x/y/z", false, true}})
	requireSparse(t, ParseSparse([]byte("!/*/\n/*\n!/*/\n"), SparseOptions{Cone: true}), []sparseCase{{"x/y", false, false}, {"r", false, true}})
	requireSparse(t, ParseSparse([]byte("/A/\n"), SparseOptions{Cone: true, IgnoreCase: true}), []sparseCase{{"a/x", false, true}, {"b/x", false, false}})
	requireSparse(t, ParseSparse([]byte("/A/\n"), SparseOptions{Cone: true}), []sparseCase{{"a/x", false, false}})
	requireSparse(t, ParseSparse([]byte("/a/\n!/a/*/\n/a/\n"), SparseOptions{Cone: true}), []sparseCase{{"a/c/y", false, true}})
	requireSparse(t, ParseSparse([]byte("/a\\?b/\n/c\\*d/\n"), SparseOptions{Cone: true}), []sparseCase{{"a?b/x", false, true}, {"c*d/x", false, true}, {"axb/x", false, false}})
}

func TestSparseConeFallsBackToPatternsItCannotRepresent(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		cases []sparseCase
	}{
		{"notAnchored", "*.txt\n", []sparseCase{{"a/x.txt", false, true}, {"a/y.md", false, false}}},
		{"tooShort", "/a/\n/\n", []sparseCase{{"a/x", false, true}}},
		{"doubleStar", "/a/**/\n", []sparseCase{{"a/b/c", false, true}}},
		{"file", "/a\n", []sparseCase{{"a", false, true}, {"b", false, false}}},
		{"glob", "/a?/\n", []sparseCase{{"ab/x", false, true}}},
		{"parentWithoutRecursive", "!/a/*/\n", []sparseCase{{"a/b/x", false, false}, {"top", false, false}}},
		{"positiveParent", "/a/*/\n", []sparseCase{{"a/b/x", false, true}}},
		{"negativeDirectory", "/a/\n!/b/\n", []sparseCase{{"a/x", false, true}, {"b/x", false, false}}},
		{"negativeRoot", "!/*\n", []sparseCase{{"top", false, false}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sparse := ParseSparse([]byte(tc.data), SparseOptions{Cone: true})
			if sparse.Cone() {
				t.Fatalf("%q kept cone matching", tc.data)
			}
			requireSparse(t, sparse, tc.cases)
		})
	}
}
