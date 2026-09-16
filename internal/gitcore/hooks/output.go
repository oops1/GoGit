package hooks

import (
	"slices"
	"strings"
)

var (
	maxLineBytes   = 4096
	maxStreamBytes = 1 << 20
	tailLines      = 20
)

type output struct {
	hook      string
	sink      Sink
	pending   []byte
	afterCR   bool
	broken    bool
	streamed  int
	truncated bool
	tail      []string
}

func newOutput(hook string, sink Sink) *output {
	return &output{hook: hook, sink: sink}
}

func (o *output) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' && o.afterCR {
			o.afterCR = false
			continue
		}
		o.afterCR = b == '\r'
		if b == '\n' || b == '\r' {
			if !o.broken {
				o.line()
			}
			o.broken = false
			continue
		}
		o.broken = false
		o.pending = append(o.pending, b)
		if len(o.pending) >= maxLineBytes {
			o.line()
			o.broken = true
		}
	}
	return len(p), nil
}

func (o *output) line() {
	text := strings.ToValidUTF8(string(o.pending), "�")
	o.pending = o.pending[:0]
	o.tail = append(o.tail, text)
	if len(o.tail) > tailLines {
		o.tail = slices.Delete(o.tail, 0, len(o.tail)-tailLines)
	}
	if o.truncated || o.streamed+len(text) > maxStreamBytes {
		if !o.truncated {
			o.truncated = true
			o.sink.emit(Event{Kind: EventTruncated, Hook: o.hook})
		}
		return
	}
	o.streamed += len(text) + 1
	o.sink.emit(Event{Kind: EventOutput, Hook: o.hook, Line: text})
}

func (o *output) finish() ([]string, bool) {
	if len(o.pending) > 0 {
		o.line()
	}
	return slices.Clone(o.tail), o.truncated
}
