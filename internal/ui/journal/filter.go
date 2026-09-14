package journal

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Filter struct {
	Branch        string
	Tip           hash.ObjectID
	Author        string
	Message       string
	Path          string
	Content       string
	ContentRegexp bool
	Since         time.Time
}

func (f Filter) Empty() bool {
	return f.Branch == "" &&
		strings.TrimSpace(f.Author) == "" &&
		strings.TrimSpace(f.Message) == "" &&
		strings.TrimSpace(f.Path) == "" &&
		strings.TrimSpace(f.Content) == "" &&
		f.Since.IsZero()
}

func (f Filter) Apply(opts Options) (Options, error) {
	opts.Tip = f.Tip
	opts.Walk.Author = substring(f.Author)
	opts.Walk.Grep = substring(f.Message)
	opts.Walk.Since = f.Since
	if path := strings.TrimSpace(f.Path); path != "" {
		opts.Walk.Paths = []string{strings.ReplaceAll(path, `\`, "/")}
	}
	if strings.TrimSpace(f.Content) == "" {
		return opts, nil
	}
	if !f.ContentRegexp {
		opts.Walk.Pickaxe = f.Content
		return opts, nil
	}
	pattern, err := regexp.Compile(f.Content)
	if err != nil {
		return opts, fmt.Errorf("journal: content pattern: %w", err)
	}
	opts.Walk.PickaxeRegexp = pattern
	return opts, nil
}

func substring(text string) *regexp.Regexp {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(text))
}
