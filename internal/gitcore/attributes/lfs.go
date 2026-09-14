package attributes

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

const (
	lfsSpecURL         = "https://git-lfs.github.com/spec/v1"
	lfsPassThroughSize = 512
	lfsPointerLimit    = 1024
	lfsOIDType         = "sha256"
)

var (
	lfsVersions   = []string{lfsSpecURL, "https://hawser.github.com/spec/v1", "http://git-media.io/v/2"}
	lfsPointerKey = []string{"version", "oid", "size"}
	lfsExtension  = regexp.MustCompile(`\Aext-\d{1}-\w+`)
	lfsOID        = regexp.MustCompile(`\A[[:alnum:]]{64}`)
	lfsMarker     = regexp.MustCompile(`git-media|hawser|git-lfs`)
)

type LFSPointer struct {
	OID      string
	Size     int64
	Extended bool
}

func DecodeLFSPointer(data []byte) (LFSPointer, bool) {
	if len(data) > lfsPointerLimit || !lfsMarker.Match(data) {
		return LFSPointer{}, false
	}
	return parseLFSPointer(bytes.TrimSpace(data))
}

func (p LFSPointer) Encode() []byte {
	if p.Size == 0 {
		return nil
	}
	return encodeLFSPointer(p.OID, p.Size)
}

func lfsClean(data []byte) ([]byte, bool) {
	if len(data) == 0 || len(data) < lfsPassThroughSize && isLFSPointer(bytes.TrimSpace(data)) {
		return data, true
	}
	return lfsPointer(sha256.Sum256(data), int64(len(data))), false
}

func lfsPointer(oid [sha256.Size]byte, size int64) []byte {
	return encodeLFSPointer(hex.EncodeToString(oid[:]), size)
}

func encodeLFSPointer(oid string, size int64) []byte {
	return fmt.Appendf(nil, "version %s\noid %s:%s\nsize %d\n", lfsSpecURL, lfsOIDType, oid, size)
}

func isLFSPointer(data []byte) bool {
	_, ok := parseLFSPointer(data)
	return ok
}

func parseLFSPointer(data []byte) (LFSPointer, bool) {
	values := map[string]string{}
	line := 0
	extended := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		key, value, ok := strings.Cut(text, " ")
		if !ok || line >= len(lfsPointerKey) {
			return LFSPointer{}, false
		}
		if key != lfsPointerKey[line] {
			if !lfsExtension.MatchString(key) || !validLFSExtension(key, value) {
				return LFSPointer{}, false
			}
			extended = true
			continue
		}
		values[key] = value
		line++
	}
	size, err := strconv.ParseInt(values["size"], 10, 64)
	if err != nil || size < 0 || !validLFSOID(values["oid"]) || !slices.Contains(lfsVersions, values["version"]) {
		return LFSPointer{}, false
	}
	return LFSPointer{OID: strings.TrimPrefix(values["oid"], lfsOIDType+":"), Size: size, Extended: extended}, true
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

type LFSFetchFilter struct {
	include []pattern
	exclude []pattern
	icase   bool
}

func NewLFSFetchFilter(cfg *config.Config, ignoreCase bool) LFSFetchFilter {
	filter := LFSFetchFilter{icase: ignoreCase}
	if cfg == nil {
		return filter
	}
	include, _ := cfg.Get("lfs.fetchinclude")
	exclude, _ := cfg.Get("lfs.fetchexclude")
	filter.include = lfsPathPatterns(include)
	filter.exclude = lfsPathPatterns(exclude)
	return filter
}

func lfsPathPatterns(raw string) []pattern {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []pattern
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		for _, separator := range []string{"/", `\`} {
			if trimmed, ok := strings.CutSuffix(part, separator); ok {
				part = trimmed
				break
			}
		}
		out = append(out, pattern{text: part, noDir: !strings.Contains(part, "/")})
	}
	return out
}

func (f LFSFetchFilter) Allows(path string) bool {
	if len(f.include) > 0 && !f.matchesAny(f.include, path) {
		return false
	}
	return !f.matchesAny(f.exclude, path)
}

func (f LFSFetchFilter) matchesAny(patterns []pattern, path string) bool {
	for _, p := range patterns {
		if p.text != "" && p.match(path, false, f.icase) {
			return true
		}
		for at := range len(path) {
			if path[at] == '/' && p.text != "" && p.match(path[:at], true, f.icase) {
				return true
			}
		}
	}
	return false
}
