package ops

import (
	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

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
	attributesFile, err := attributesFileOf(r)
	if err != nil {
		return diff.Options{}, err
	}
	work := attributes.MemoryLoader(nil)
	if r.WorkTree() != "" {
		work = attributes.OSLoader(r.WorkTree())
	}
	attrs := attributes.New(attributes.AttributeOptions{
		Work:           work,
		Global:         attributes.OSLoader(""),
		InfoFile:       r.CommonPath("info/attributes"),
		AttributesFile: attributesFile,
		IgnoreCase:     r.Core().IgnoreCase,
		Config:         r.Config(),
	})
	opts.BinaryHint = attrs.DiffBinary
	return opts, nil
}
