package app

import "github.com/oops1/gogit/internal/logx"

type redactedError struct {
	err error
}

func (e redactedError) Error() string {
	return logx.RedactText(e.err.Error())
}

func (e redactedError) Unwrap() error {
	return e.err
}

func redactError(err error) error {
	if err == nil {
		return nil
	}
	return redactedError{err: err}
}
