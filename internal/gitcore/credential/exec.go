package credential

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const helperTimeout = 30 * time.Second

const maxAnswerBytes = 1 << 20

var errHelperOutputTooLarge = errors.New("credential: helper output exceeds the size limit")

type execHelper struct {
	name string
	exe  string
}

func (h *execHelper) Name() string { return h.name }

func (h *execHelper) Get(ctx context.Context, q Query) (Answer, bool, error) {
	request := encodeRequest(q, nil)
	out, err := h.run(ctx, "get", request)
	clear(request)
	if err != nil {
		return Answer{}, false, err
	}
	ans, complete, err := decodeAnswer(out)
	clear(out)
	if err != nil {
		return Answer{}, false, err
	}
	if !complete {
		return Answer{}, false, nil
	}
	return ans, true, nil
}

func (h *execHelper) Store(ctx context.Context, q Query, a Answer) error {
	request := encodeRequest(q, a.Password)
	out, err := h.run(ctx, "store", request)
	clear(request)
	clear(out)
	return err
}

func (h *execHelper) Erase(ctx context.Context, q Query) error {
	request := encodeRequest(q, nil)
	out, err := h.run(ctx, "erase", request)
	clear(request)
	clear(out)
	return err
}

func (h *execHelper) run(ctx context.Context, action string, request []byte) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, h.exe, action)
	cmd.Stdin = bytes.NewReader(request)
	stdout := boundedBuffer{limit: maxAnswerBytes}
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		clear(stdout.data)
		return nil, fmt.Errorf("%w: %s: %w", ErrHelperFailed, h.name, err)
	}
	return stdout.data, nil
}

type boundedBuffer struct {
	data  []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		return 0, errHelperOutputTooLarge
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func encodeRequest(q Query, password []byte) []byte {
	var buf bytes.Buffer
	writeField(&buf, "protocol", q.Protocol)
	writeField(&buf, "host", q.Host)
	writeField(&buf, "path", q.Path)
	writeField(&buf, "username", q.Username)
	if password != nil {
		buf.WriteString("password=")
		buf.Write(password)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

func writeField(buf *bytes.Buffer, key, value string) {
	if value == "" {
		return
	}
	buf.WriteString(key)
	buf.WriteByte('=')
	buf.WriteString(value)
	buf.WriteByte('\n')
}

func decodeAnswer(data []byte) (Answer, bool, error) {
	var ans Answer
	gotUsername, gotPassword := false, false
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), maxAnswerBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		key, value, ok := bytes.Cut(line, []byte{'='})
		if !ok || len(key) == 0 {
			return Answer{}, false, ErrMalformedAnswer
		}
		switch string(key) {
		case "username":
			ans.Username = string(value)
			gotUsername = true
		case "password":
			ans.Password = append([]byte(nil), value...)
			gotPassword = true
		}
	}
	if err := sc.Err(); err != nil {
		return Answer{}, false, fmt.Errorf("%w: %w", ErrMalformedAnswer, err)
	}
	return ans, gotUsername && gotPassword, nil
}
