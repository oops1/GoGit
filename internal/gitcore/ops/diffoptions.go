package ops

import (
	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/userdiff"
)

const diffAttribute = "diff"

func repoDiffOptions(r *repo.Repository, opts diff.Options) (diff.Options, error) {
	if opts.RenameThreshold == 0 {
		opts = diff.Defaults()
		if name, set := r.Config().Get("diff.algorithm"); set {
			algorithm, err := diff.ParseAlgorithm(name)
			if err != nil {
				return diff.Options{}, err
			}
			opts.Algorithm = algorithm
		}
	}
	if opts.BinaryHint != nil {
		return opts, nil
	}
	attrs, err := repoAttributes(r)
	if err != nil {
		return diff.Options{}, err
	}
	opts.BinaryHint = attrs.DiffBinary
	opts.FuncNames = diffFuncNames(attrs, userdiff.Load(r.Config()))
	return opts, nil
}

func repoAttributes(r *repo.Repository) (*attributes.Attributes, error) {
	attributesFile, err := attributesFileOf(r)
	if err != nil {
		return nil, err
	}
	work := attributes.MemoryLoader(nil)
	if r.WorkTree() != "" {
		work = attributes.OSLoader(r.WorkTree())
	}
	return attributes.New(attributes.AttributeOptions{
		Work:           work,
		Global:         attributes.OSLoader(""),
		InfoFile:       r.CommonPath("info/attributes"),
		AttributesFile: attributesFile,
		IgnoreCase:     r.Core().IgnoreCase,
		Config:         r.Config(),
	}), nil
}

func diffFuncNames(attrs *attributes.Attributes, drivers *userdiff.Drivers) diff.FuncNames {
	return func(path string) *userdiff.Matcher {
		return diffDriverMatcher(drivers, attrs.Get(path, diffAttribute)[diffAttribute])
	}
}

func diffDriverMatcher(drivers *userdiff.Drivers, value attributes.Value) *userdiff.Matcher {
	name := userdiff.DefaultDriver
	switch {
	case value.IsSet(), value.IsUnset():
		return nil
	case value.Kind() == attributes.Valued:
		if _, known := drivers.Driver(value.Text()); known {
			name = value.Text()
		}
	}
	matcher, err := drivers.Matcher(name)
	if err != nil {
		return nil
	}
	return matcher
}
