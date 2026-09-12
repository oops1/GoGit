package journal

import (
	"regexp"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Filter struct {
	Branch  string
	Tip     hash.ObjectID
	Author  string
	Message string
}

func (f Filter) Empty() bool {
	return f.Branch == "" && strings.TrimSpace(f.Author) == "" && strings.TrimSpace(f.Message) == ""
}

func (f Filter) Apply(opts Options) Options {
	opts.Tip = f.Tip
	opts.Walk.Author = substring(f.Author)
	opts.Walk.Grep = substring(f.Message)
	return opts
}

func substring(text string) *regexp.Regexp {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(text))
}
