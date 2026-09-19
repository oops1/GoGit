package console

import (
	"errors"
	"strings"
)

var (
	ErrBadQuoting = errors.New("console: the command line ends inside a quoted word")
	ErrBadEscape  = errors.New("console: the command line ends with a backslash")
)

const gitWord = "git"

type Command struct {
	Name string
	Args []string
}

func (c Command) Empty() bool { return c.Name == "" }

type splitter struct {
	words   []string
	word    strings.Builder
	started bool
	quote   rune
	escape  bool
}

func Split(line string) ([]string, error) {
	s := &splitter{}
	for _, r := range line {
		s.feed(r)
	}
	if s.quote != 0 {
		return nil, ErrBadQuoting
	}
	if s.escape {
		return nil, ErrBadEscape
	}
	s.flush()
	return s.words, nil
}

func (s *splitter) feed(r rune) {
	switch {
	case s.escape:
		s.keep(r)
		s.escape = false
	case s.quote == '\'':
		s.inSingle(r)
	case s.quote == '"':
		s.inDouble(r)
	case r == '\'' || r == '"':
		s.quote = r
		s.started = true
	case r == '\\':
		s.escape = true
	case isSpace(r):
		s.flush()
	default:
		s.keep(r)
	}
}

func (s *splitter) inSingle(r rune) {
	if r == '\'' {
		s.quote = 0
		return
	}
	s.keep(r)
}

func (s *splitter) inDouble(r rune) {
	switch r {
	case '"':
		s.quote = 0
	case '\\':
		s.escape = true
	default:
		s.keep(r)
	}
}

func (s *splitter) keep(r rune) {
	s.word.WriteRune(r)
	s.started = true
}

func (s *splitter) flush() {
	if !s.started {
		return
	}
	s.words = append(s.words, s.word.String())
	s.word.Reset()
	s.started = false
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func Parse(line string) (Command, error) {
	words, err := Split(line)
	if err != nil {
		return Command{}, err
	}
	if len(words) > 0 && words[0] == gitWord {
		words = words[1:]
	}
	if len(words) == 0 {
		return Command{}, nil
	}
	return Command{Name: words[0], Args: words[1:]}, nil
}
