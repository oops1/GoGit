package ops

import (
	"bytes"
	"errors"
	"os"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type workingTree struct {
	root     *os.Root
	ignore   *attributes.Matcher
	attrs    *attributes.Attributes
	fileMode bool
}

func openWorkingTree(r *repo.Repository) (*workingTree, error) {
	if r.IsBare() || r.WorkTree() == "" {
		return nil, ErrBareRepository
	}
	root, err := fsOpenRoot(r.WorkTree())
	if err != nil {
		return nil, err
	}
	core := r.Core()
	excludesFile := excludesFileOf(r)
	attributesFile, err := attributesFileOf(r)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	ignore := attributes.NewMatcher(attributes.IgnoreOptions{
		Work:         attributes.RootLoader(root),
		Global:       attributes.OSLoader(""),
		InfoExclude:  r.InfoExclude(),
		ExcludesFile: excludesFile,
		IgnoreCase:   core.IgnoreCase,
	})
	attrs := attributes.New(attributes.AttributeOptions{
		Work:           attributes.RootLoader(root),
		Global:         attributes.OSLoader(""),
		InfoFile:       r.CommonPath("info/attributes"),
		AttributesFile: attributesFile,
		IgnoreCase:     core.IgnoreCase,
		AutoCRLF:       core.AutoCRLF,
		EOL:            core.EOL,
	})
	return &workingTree{root: root, ignore: ignore, attrs: attrs, fileMode: core.FileMode}, nil
}

func (w *workingTree) close() error {
	return w.root.Close()
}

func excludesFileOf(r *repo.Repository) string {
	if r.Core().ExcludesFile != "" {
		return r.Core().ExcludesFile
	}
	return attributes.DefaultExcludesFile(os.Getenv)
}

func attributesFileOf(r *repo.Repository) (string, error) {
	configured, err := r.Config().GetPath("core.attributesfile")
	if errors.Is(err, config.ErrNotFound) {
		return attributes.DefaultAttributesFile(os.Getenv), nil
	}
	if err != nil {
		return "", err
	}
	if configured != "" {
		return configured, nil
	}
	return attributes.DefaultAttributesFile(os.Getenv), nil
}

func (w *workingTree) isIgnored(path string, isDir bool) bool {
	ignored, _ := w.ignore.Ignored(path, isDir)
	return ignored
}

func (w *workingTree) checkinConvert(path string, data []byte) []byte {
	policy := w.attrs.Text(path)
	if policy.Convert.OnCheckin != attributes.ConvertLF {
		return data
	}
	if policy.Convert.Detect && attributes.IsBinaryContent(data) {
		return data
	}
	if !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func (w *workingTree) checkoutConvert(path string, data []byte) []byte {
	policy := w.attrs.Text(path)
	if policy.Convert.OnCheckout != attributes.ConvertCRLF {
		return data
	}
	if policy.Convert.Detect && attributes.IsBinaryContent(data) {
		return data
	}
	if bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
}
