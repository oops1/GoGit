package config

import "errors"

var (
	ErrSyntax            = errors.New("gitconfig: syntax error")
	ErrBadSection        = errors.New("gitconfig: bad section header")
	ErrBadKey            = errors.New("gitconfig: bad key")
	ErrBadEscape         = errors.New("gitconfig: bad escape sequence")
	ErrUnterminatedQuote = errors.New("gitconfig: unterminated quote")
	ErrInvalidName       = errors.New("gitconfig: invalid variable name")
	ErrInvalidSection    = errors.New("gitconfig: invalid section name")
	ErrNotFound          = errors.New("gitconfig: no such variable")
	ErrSectionNotFound   = errors.New("gitconfig: no such section")
	ErrIncludeDepth      = errors.New("gitconfig: include depth exceeded")
	ErrIncludeCycle      = errors.New("gitconfig: include cycle")
	ErrInvalidBool       = errors.New("gitconfig: invalid boolean value")
	ErrInvalidInt        = errors.New("gitconfig: invalid integer value")
	ErrInvalidValue      = errors.New("gitconfig: invalid value")
	ErrMissingValue      = errors.New("gitconfig: missing value")
	ErrExpandUser        = errors.New("gitconfig: cannot expand user path")
	ErrUnknownExtension  = errors.New("gitconfig: unknown repository extension")
	ErrInvalidEnvCount   = errors.New("gitconfig: invalid GIT_CONFIG_COUNT")
	ErrMissingEnvEntry   = errors.New("gitconfig: missing GIT_CONFIG_KEY or GIT_CONFIG_VALUE")
)
