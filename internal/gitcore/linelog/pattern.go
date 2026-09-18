package linelog

import (
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/posixre"
)

func compilePattern(pattern string) (*posixre.Regexp, error) {
	re, err := posixre.Compile(pattern, posixre.Newline)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPattern, err)
	}
	return re, nil
}
