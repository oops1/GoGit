package object

import (
	"bytes"
	"fmt"
	"strings"
)

const expectedHeaders = 6

type ExtraHeader struct {
	Key   string
	Value string
}

func splitHeaders(data []byte) ([]ExtraHeader, string, error) {
	separator := bytes.Index(data, []byte("\n\n"))
	if separator < 0 {
		return nil, "", fmt.Errorf("%w: no blank line between headers and message", ErrMalformed)
	}
	block := string(data[:separator+1])
	message := string(data[separator+2:])
	headers := make([]ExtraHeader, 0, expectedHeaders)
	for len(block) > 0 {
		var line string
		line, block, _ = strings.Cut(block, "\n")
		if len(line) > 0 && line[0] == ' ' {
			if len(headers) == 0 {
				return nil, "", fmt.Errorf("%w: continuation line before any header", ErrMalformed)
			}
			headers[len(headers)-1].Value += "\n" + line[1:]
			continue
		}
		key, value, found := strings.Cut(line, " ")
		if !found {
			return nil, "", fmt.Errorf("%w: header line %q has no value", ErrMalformed, line)
		}
		headers = append(headers, ExtraHeader{Key: key, Value: value})
	}
	return headers, message, nil
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteByte(' ')
	buf.WriteString(strings.ReplaceAll(value, "\n", "\n "))
	buf.WriteByte('\n')
}

type headerSet map[string]struct{}

func (s headerSet) add(key string) error {
	if _, exists := s[key]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateHeader, key)
	}
	s[key] = struct{}{}
	return nil
}

func (s headerSet) require(keys ...string) error {
	for _, key := range keys {
		if _, exists := s[key]; !exists {
			return fmt.Errorf("%w: %s", ErrMissingHeader, key)
		}
	}
	return nil
}
