package posixre

import (
	"regexp"
	"unicode/utf8"
)

type Regexp struct {
	re *regexp.Regexp
}

func (r *Regexp) String() string { return r.re.String() }

func (r *Regexp) NumSubexp() int { return r.re.NumSubexp() }

func (r *Regexp) Match(data []byte) bool {
	return r.re.Match(NewSubject(data).wide)
}

func (r *Regexp) FindIndex(data []byte) []int {
	return NewSubject(data).FindIndex(r, 0)
}

func (r *Regexp) FindSubmatchIndex(data []byte) []int {
	return NewSubject(data).FindSubmatchIndex(r, 0)
}

type Subject struct {
	wide   []byte
	toWide []int
	toData []int
}

func NewSubject(data []byte) *Subject {
	high := 0
	for _, c := range data {
		if c >= utf8.RuneSelf {
			high++
		}
	}
	if high == 0 {
		return &Subject{wide: data}
	}
	s := &Subject{
		wide:   make([]byte, 0, len(data)+high),
		toWide: make([]int, 0, len(data)+1),
		toData: make([]int, 0, len(data)+high+1),
	}
	for at, c := range data {
		s.toWide = append(s.toWide, len(s.wide))
		if c < utf8.RuneSelf {
			s.wide = append(s.wide, c)
			s.toData = append(s.toData, at)
			continue
		}
		s.wide = utf8.AppendRune(s.wide, rune(c))
		s.toData = append(s.toData, at, at)
	}
	s.toWide = append(s.toWide, len(s.wide))
	s.toData = append(s.toData, len(data))
	return s
}

func (s *Subject) FindIndex(r *Regexp, from int) []int {
	start := s.wideOffset(from)
	return s.narrow(r.re.FindIndex(s.wide[start:]), start)
}

func (s *Subject) FindSubmatchIndex(r *Regexp, from int) []int {
	start := s.wideOffset(from)
	return s.narrow(r.re.FindSubmatchIndex(s.wide[start:]), start)
}

func (s *Subject) wideOffset(at int) int {
	if s.toWide == nil {
		return at
	}
	return s.toWide[at]
}

func (s *Subject) narrow(loc []int, start int) []int {
	for at, offset := range loc {
		switch {
		case offset < 0:
		case s.toData == nil:
			loc[at] = offset + start
		default:
			loc[at] = s.toData[offset+start]
		}
	}
	return loc
}
