package i18n

import (
	"errors"
	"io/fs"
	"testing/fstest"
)

type openFailFS struct {
	inner fstest.MapFS
}

func (f openFailFS) Open(name string) (fs.File, error) {
	if name == "." {
		return f.inner.Open(name)
	}
	return nil, errors.New("open failed")
}
