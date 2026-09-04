package repo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (r *Repository) Shallow() (map[hash.ObjectID]struct{}, error) {
	return readShallow(r.commonRoot, shallowFileName)
}

func readShallow(root *os.Root, rel string) (map[hash.ObjectID]struct{}, error) {
	data, err := root.ReadFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return map[hash.ObjectID]struct{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseShallow(data)
}

func parseShallow(data []byte) (map[hash.ObjectID]struct{}, error) {
	shallow := make(map[hash.ObjectID]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		id, err := hash.Parse(line)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrInvalidShallowFile, line, err)
		}
		shallow[id] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidShallowFile, err)
	}
	return shallow, nil
}
