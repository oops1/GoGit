package attributes

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	lfsSpecURL         = "https://git-lfs.github.com/spec/v1"
	lfsPassThroughSize = 512
	lfsOIDType         = "sha256"
)

var (
	lfsVersions   = []string{lfsSpecURL, "https://hawser.github.com/spec/v1", "http://git-media.io/v/2"}
	lfsPointerKey = []string{"version", "oid", "size"}
	lfsExtension  = regexp.MustCompile(`\Aext-\d{1}-\w+`)
	lfsOID        = regexp.MustCompile(`\A[[:alnum:]]{64}`)
)

func lfsClean(data []byte) ([]byte, bool) {
	if len(data) == 0 || len(data) < lfsPassThroughSize && isLFSPointer(bytes.TrimSpace(data)) {
		return data, true
	}
	return lfsPointer(sha256.Sum256(data), int64(len(data))), false
}

func lfsPointer(oid [sha256.Size]byte, size int64) []byte {
	return fmt.Appendf(nil, "version %s\noid %s:%x\nsize %d\n", lfsSpecURL, lfsOIDType, oid, size)
}

func isLFSPointer(data []byte) bool {
	values := map[string]string{}
	line := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		key, value, ok := strings.Cut(text, " ")
		if !ok || line >= len(lfsPointerKey) {
			return false
		}
		if key != lfsPointerKey[line] {
			if !lfsExtension.MatchString(key) || !validLFSExtension(key, value) {
				return false
			}
			continue
		}
		values[key] = value
		line++
	}
	size, err := strconv.ParseInt(values["size"], 10, 64)
	return err == nil && size >= 0 && validLFSOID(values["oid"]) && slices.Contains(lfsVersions, values["version"])
}

func validLFSOID(value string) bool {
	kind, oid, ok := strings.Cut(value, ":")
	return ok && kind == lfsOIDType && lfsOID.MatchString(oid)
}

func validLFSExtension(key, value string) bool {
	parts := strings.SplitN(key, "-", 3)
	priority, err := strconv.Atoi(parts[1])
	return err == nil && priority >= 0 && validLFSOID(value)
}
