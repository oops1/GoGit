//go:build oracle

package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/config"
)

const oracleAttributes = `*.nodiff -diff
*.bin binary
*.forced diff=forced
*.text diff=text
*.auto diff=auto
*.set diff
*.named diff=unconfigured
`

func crlfText(prefix string, count int, ending string) string {
	var out strings.Builder
	for at := range count {
		out.WriteString(prefix + " line " + strings.Repeat("x", at%5) + ending)
	}
	return out.String()
}

func attributeTrees() (treeFiles, treeFiles) {
	old := treeFiles{
		"a.nodiff":       blobSpec("one\ntwo\n"),
		"b.bin":          blobSpec("one\n"),
		"c.forced":       blobSpec("plain\n"),
		"d.text":         blobSpec("nul\x00here\nsecond\n"),
		"e.auto":         blobSpec("x\x00\n"),
		"f.set":          blobSpec("text\n"),
		"g.named":        blobSpec("named\n"),
		"h.txt":          blobSpec("zero\x00\n"),
		"crlf.nodiff":    blobSpec(crlfText("both", 40, "\r\n")),
		"half.nodiff":    blobSpec(crlfText("half", 40, "\r\n")),
		"text-crlf.txt":  blobSpec(crlfText("text", 40, "\r\n")),
		"removed.nodiff": blobSpec("gone\n"),
		"plain.nodiff":   blobSpec(crlfText("plain", 40, "\n")),
		"back.txt":       blobSpec(crlfText("back", 40, "\n")),
	}
	updated := treeFiles{
		"a.nodiff":      blobSpec("one\nTWO\n"),
		"b.bin":         blobSpec("two\n"),
		"c.forced":      blobSpec("PLAIN\n"),
		"d.text":        blobSpec("nul\x00here\nSECOND\n"),
		"e.auto":        blobSpec("y\x00\n"),
		"f.set":         blobSpec("TEXT\n"),
		"g.named":       blobSpec("NAMED\n"),
		"h.txt":         blobSpec("one\x00\n"),
		"lf.nodiff":     blobSpec(crlfText("both", 40, "\n")),
		"half.txt":      blobSpec(crlfText("half", 40, "\n")),
		"text-lf.txt":   blobSpec(crlfText("text", 40, "\n")),
		"added.nodiff":  blobSpec("fresh\n"),
		"added-bin.txt": blobSpec("fresh\x00\n"),
		"plain.txt":     blobSpec(crlfText("plain", 39, "\n") + "changed\n"),
		"back.nodiff":   blobSpec(crlfText("back", 39, "\n") + "changed\n"),
	}
	return old, updated
}

func TestDiffAttributesMatchGit(t *testing.T) {
	o := newOracle(t)
	info := filepath.Join(o.repo, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(filepath.Join(info, "attributes"), []byte(oracleAttributes), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	o.run(o.repo, nil, "config", "diff.forced.binary", "true")
	o.run(o.repo, nil, "config", "diff.text.binary", "false")
	o.run(o.repo, nil, "config", "diff.auto.binary", "auto")
	cfg, err := config.Load(config.Options{GitDir: o.repo, NoSystem: true, GlobalFile: filepath.Join(o.home, ".gitconfig")})
	if err != nil {
		t.Fatalf("config.Load returned error %v", err)
	}
	attrs := attributes.New(attributes.AttributeOptions{
		Global:   attributes.OSLoader(""),
		InfoFile: filepath.Join(info, "attributes"),
		Config:   cfg,
	})
	db := o.objects()
	oldFiles, newFiles := attributeTrees()
	oldTree, newTree := buildTree(o, oldFiles), buildTree(o, newFiles)
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	for _, v := range []treeVariant{
		{name: "patch", args: []string{"-p", "-M"}, opts: Defaults()},
		{name: "patch-copies", args: []string{"-p", "-M", "-C", "--find-copies-harder"}, opts: withOptions(func(o *Options) { o.DetectCopies = true })},
		{name: "stat", args: []string{"--stat", "-M"}, kind: variantStat, opts: Defaults()},
		{name: "numstat", args: []string{"--numstat", "-M"}, kind: variantNumStat, opts: Defaults()},
	} {
		t.Run(v.name, func(t *testing.T) {
			v.opts.BinaryHint = attrs.DiffBinary
			want := o.treeDiff(oldTree, newTree, v.args)
			files, err := Trees(t.Context(), db, oldTree, newTree, v.opts)
			if err != nil {
				t.Fatalf("Trees returned error %v", err)
			}
			if got := renderFiles(t, files, v.kind, v.opts); got != want {
				t.Errorf("output differs from git\n--- git ---\n%s\n--- ours ---\n%s", want, got)
			}
		})
	}
}
