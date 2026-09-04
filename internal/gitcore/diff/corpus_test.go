package diff

import (
	"fmt"
	"strings"
)

type corpusPair struct {
	name string
	old  string
	new  string
}

func repeatLines(prefix string, count int) string {
	var out strings.Builder
	for at := range count {
		fmt.Fprintf(&out, "%s%d\n", prefix, at)
	}
	return out.String()
}

func ignorableBlankMix() string {
	changed := map[int]string{0: "CHANGED 0\n", 9: "CHANGED 9\n", 19: "CHANGED 19\n"}
	blanksBefore := map[int]int{4: 1, 5: 1, 13: 1, 14: 1, 15: 1}
	var out strings.Builder
	for at := range 24 {
		for range blanksBefore[at] {
			out.WriteString("\n")
		}
		if record, ok := changed[at]; ok {
			out.WriteString(record)
			continue
		}
		fmt.Fprintf(&out, "line %d\n", at)
	}
	return out.String()
}

func corpus() []corpusPair {
	big := repeatLines("line ", 1000)
	bigChanged := strings.Replace(big, "line 500\n", "changed 500\n", 1)
	return []corpusPair{
		{"empty-both", "", ""},
		{"empty-to-text", "", "alpha\nbeta\n"},
		{"text-to-empty", "alpha\nbeta\n", ""},
		{"identical", "alpha\nbeta\ngamma\n", "alpha\nbeta\ngamma\n"},
		{"single-line", "alpha\nbeta\ngamma\n", "alpha\nBETA\ngamma\n"},
		{"myers-abcabba", "a\nb\nc\na\nb\nb\na\n", "c\nb\na\nb\na\nc\n"},
		{"insert-head", "b\nc\nd\n", "a\nb\nc\nd\n"},
		{"insert-tail", "a\nb\nc\n", "a\nb\nc\nd\n"},
		{"delete-middle", "a\nb\nc\nd\ne\n", "a\nb\ne\n"},
		{"replace-all", "one\ntwo\nthree\n", "four\nfive\nsix\n"},
		{"no-newline-old", "a\nb\nc", "a\nb\nc\n"},
		{"no-newline-new", "a\nb\nc\n", "a\nb\nc"},
		{"no-newline-both", "a\nb\nc", "a\nb\nd"},
		{"no-newline-single", "solo", "solo two"},
		{"crlf-to-lf", "a\r\nb\r\nc\r\n", "a\nb\nc\n"},
		{"lf-to-crlf", "a\nb\nc\n", "a\r\nb\r\nc\r\n"},
		{"crlf-changed", "a\r\nb\r\nc\r\n", "a\r\nB\r\nc\r\n"},
		{"trailing-space", "a\nb\nc\n", "a \nb\t\nc\n"},
		{"internal-space", "a b c\nd e f\n", "a  b   c\nd\te\tf\n"},
		{"leading-space", "a\n  b\nc\n", "  a\nb\n\tc\n"},
		{"blank-lines-added", "a\nb\nc\n", "a\n\n\nb\n\nc\n"},
		{"blank-lines-removed", "a\n\n\nb\n\nc\n", "a\nb\nc\n"},
		{"blank-and-real", "a\nb\nc\nd\ne\nf\ng\nh\n", "a\n\nb\nc\nd\ne\nf\nCHANGED\nh\n"},
		{"repeated-lines", "x\nx\nx\nx\ny\nx\nx\nx\n", "x\nx\ny\nx\nx\nx\nx\nx\n"},
		{"block-move", "one\ntwo\nthree\nfour\nfive\nsix\n", "four\nfive\nsix\none\ntwo\nthree\n"},
		{
			"indent-shift",
			"func a() {\n\tcall()\n}\n\nfunc b() {\n\tcall()\n}\n",
			"func a() {\n\tcall()\n}\n\nfunc mid() {\n\tcall()\n}\n\nfunc b() {\n\tcall()\n}\n",
		},
		{
			"indent-blank",
			"if (x) {\n    a();\n}\n\nif (y) {\n    b();\n}\n",
			"if (x) {\n    a();\n}\n\nif (z) {\n    c();\n}\n\nif (y) {\n    b();\n}\n",
		},
		{"duplicated-block", "a\nb\nc\nd\n", "a\nb\nc\nd\na\nb\nc\nd\n"},
		{
			"many-hunks",
			"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n",
			"1\nX\n3\n4\n5\n6\n7\n8\nY\n10\n11\n12\n13\n14\n15\nZ\n17\n18\n19\n20\n",
		},
		{
			"close-hunks",
			"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n",
			"1\nX\n3\n4\n5\nY\n7\n8\n9\n10\n",
		},
		{"unicode", "привет\nмир\n", "привет\nвсем\n"},
		{"long-lines", strings.Repeat("x", 300) + "\n", strings.Repeat("x", 150) + "y" + strings.Repeat("x", 149) + "\n"},
		{"tabs-only", "\ta\n\t\tb\n", "        a\n                b\n"},
		{"leading-duplicate-removed", "x\nx\ny\n", "x\ny\n"},
		{"leading-duplicate-added", "x\ny\n", "x\nx\ny\n"},
		{"trailing-duplicate-added", "y\nx\n", "y\nx\nx\n"},
		{"blank-run-grows", "a\n" + strings.Repeat("\n", 25) + "b\n", "a\n" + strings.Repeat("\n", 26) + "b\n"},
		{"blank-run-shrinks", "a\n" + strings.Repeat("\n", 26) + "b\n", "a\n" + strings.Repeat("\n", 25) + "b\n"},
		{
			"blank-run-around-a-change",
			"a\n" + strings.Repeat("\n", 25) + "b\n" + strings.Repeat("\n", 25) + "c\n",
			"a\n" + strings.Repeat("\n", 25) + "B\n" + strings.Repeat("\n", 25) + "c\n",
		},
		{
			"nested-indent",
			"class C {\n  void a() {\n    x();\n  }\n\n  void b() {\n    y();\n  }\n}\n",
			"class C {\n  void a() {\n    x();\n  }\n\n  void mid() {\n    z();\n  }\n\n  void b() {\n    y();\n  }\n}\n",
		},
		{
			"deeper-indent",
			"top\n    deep\n    deep2\nbottom\n    tail\n",
			"top\n    deep\n    deep2\n    deep3\nbottom\n    tail\n",
		},
		{
			"outdent-block",
			"def a():\n    one\n    two\n\ndef b():\n    three\n",
			"def a():\n    one\n\ndef b():\n    three\n    two\n",
		},
		{
			"indent-with-blank-runs",
			"a\n\n\n\n    b\n\n\n\n    c\n",
			"a\n\n\n\n    b\n\n\n\n    b2\n\n\n\n    c\n",
		},
		{
			"blank-inserts-and-a-change",
			"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n",
			"a\n\nb\nc\n\nd\ne\nf\ng\nh\ni\nj\nk\nl\nCHANGED\nn\no\np\n",
		},
		{
			"only-blank-inserts",
			"a\nb\nc\nd\ne\nf\ng\nh\n",
			"\na\n\nb\n\nc\nd\n\n\ne\nf\ng\nh\n\n",
		},
		{
			"blank-runs-far-apart",
			repeatLines("line ", 40),
			strings.Replace(
				strings.Replace(repeatLines("line ", 40), "line 5\n", "line 5\n\n\n", 1),
				"line 30\n", "line 30\n\n\n", 1),
		},
		{
			"ignorable-blanks-between-changes",
			repeatLines("line ", 24),
			ignorableBlankMix(),
		},
		{"large-identical", big, big},
		{"large-one-change", big, bigChanged},
		{"large-truncated", big, repeatLines("line ", 500)},
	}
}

type variantKind uint8

const (
	variantPatch variantKind = iota
	variantStat
	variantNumStat
)

type variant struct {
	name string
	args []string
	kind variantKind
	opts Options
}

func withOptions(change func(*Options)) Options {
	opts := Defaults()
	change(&opts)
	return opts
}

func variants() []variant {
	return []variant{
		{name: "default", opts: Defaults()},
		{name: "histogram", args: []string{"--histogram"}, opts: withOptions(func(o *Options) { o.Algorithm = AlgorithmHistogram })},
		{name: "ignore-all-space", args: []string{"-w"}, opts: withOptions(func(o *Options) { o.IgnoreWhitespace = IgnoreAllSpace })},
		{name: "ignore-space-change", args: []string{"-b"}, opts: withOptions(func(o *Options) { o.IgnoreWhitespace = IgnoreSpaceChange })},
		{
			name: "ignore-space-at-eol",
			args: []string{"--ignore-space-at-eol"},
			opts: withOptions(func(o *Options) { o.IgnoreWhitespace = IgnoreSpaceAtEOL }),
		},
		{
			name: "ignore-blank-lines",
			args: []string{"--ignore-blank-lines"},
			opts: withOptions(func(o *Options) { o.IgnoreWhitespace = IgnoreBlankLines }),
		},
		{
			name: "ignore-blank-lines-histogram",
			args: []string{"--ignore-blank-lines", "--histogram"},
			opts: withOptions(func(o *Options) {
				o.IgnoreWhitespace = IgnoreBlankLines
				o.Algorithm = AlgorithmHistogram
			}),
		},
		{name: "context1", args: []string{"-U1"}, opts: withOptions(func(o *Options) { o.Context = 1 })},
		{name: "context0", args: []string{"-U0"}, opts: withOptions(func(o *Options) { o.Context = 0 })},
		{
			name: "interhunk2",
			args: []string{"-U1", "--inter-hunk-context=2"},
			opts: withOptions(func(o *Options) {
				o.Context = 1
				o.InterHunkContext = 2
			}),
		},
		{
			name: "no-indent-heuristic",
			args: []string{"--no-indent-heuristic"},
			opts: withOptions(func(o *Options) { o.IndentHeuristic = false }),
		},
		{name: "stat", args: []string{"--stat"}, kind: variantStat, opts: Defaults()},
		{name: "numstat", args: []string{"--numstat"}, kind: variantNumStat, opts: Defaults()},
	}
}
