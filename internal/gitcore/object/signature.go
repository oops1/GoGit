package object

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const zoneTokenLength = 5

type Signature struct {
	Name     string
	Email    string
	When     time.Time
	OmitZone bool
}

func ParseSignature(line []byte) (Signature, error) {
	return parseSignature(string(line))
}

func parseSignature(line string) (Signature, error) {
	open := strings.LastIndexByte(line, '<')
	if open < 1 || line[open-1] != ' ' {
		return Signature{}, fmt.Errorf("%w: no name before email in %q", ErrInvalidSignature, line)
	}
	closing := strings.IndexByte(line[open:], '>')
	if closing < 0 {
		return Signature{}, fmt.Errorf("%w: email is not closed in %q", ErrInvalidSignature, line)
	}
	closing += open
	rest := line[closing+1:]
	if len(rest) == 0 || rest[0] != ' ' {
		return Signature{}, fmt.Errorf("%w: no timestamp in %q", ErrInvalidSignature, line)
	}
	stamp, zone, hasZone := strings.Cut(rest[1:], " ")
	seconds, ok := parseCanonicalSeconds(stamp)
	if !ok {
		return Signature{}, invalidTimestamp(stamp)
	}
	signature := Signature{
		Name:     line[:open-1],
		Email:    line[open+1 : closing],
		OmitZone: !hasZone,
	}
	if !hasZone {
		signature.When = time.Unix(seconds, 0).UTC()
		return signature, nil
	}
	offset, err := parseZone(zone)
	if err != nil {
		return Signature{}, err
	}
	signature.When = time.Unix(seconds, 0).In(zoneLocation(zone, offset))
	return signature, nil
}

func parseCanonicalSeconds(stamp string) (int64, bool) {
	digits, negative := stamp, false
	if strings.HasPrefix(stamp, "-") {
		digits, negative = stamp[1:], true
	}
	if len(digits) == 0 || len(digits) > maxTimestampDigits || (digits[0] == '0' && (len(digits) > 1 || negative)) {
		return 0, false
	}
	var magnitude uint64
	for index := range len(digits) {
		if digits[index] < '0' || digits[index] > '9' {
			return 0, false
		}
		magnitude = magnitude*10 + uint64(digits[index]-'0')
	}
	if negative {
		if magnitude > 1<<63 {
			return 0, false
		}
		return -int64(magnitude), true
	}
	if magnitude > math.MaxInt64 {
		return 0, false
	}
	return int64(magnitude), true
}

func invalidTimestamp(stamp string) error {
	if _, err := strconv.ParseInt(stamp, 10, 64); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	return fmt.Errorf("%w: timestamp %q is not canonical", ErrInvalidSignature, stamp)
}

const (
	maxTimestampDigits = 19
	zoneCacheLimit     = 4096
)

var zoneCache = struct {
	sync.RWMutex
	locations map[string]*time.Location
}{locations: make(map[string]*time.Location)}

func zoneLocation(token string, offset int) *time.Location {
	zoneCache.RLock()
	location, cached := zoneCache.locations[token]
	zoneCache.RUnlock()
	if cached {
		return location
	}
	name := strings.Clone(token)
	location = time.FixedZone(name, offset)
	zoneCache.Lock()
	defer zoneCache.Unlock()
	if len(zoneCache.locations) < zoneCacheLimit {
		zoneCache.locations[name] = location
	}
	return location
}

func (s Signature) String() string {
	var text strings.Builder
	text.WriteString(s.Name)
	text.WriteString(" <")
	text.WriteString(s.Email)
	text.WriteString("> ")
	text.WriteString(strconv.FormatInt(s.When.Unix(), 10))
	if s.OmitZone {
		return text.String()
	}
	text.WriteByte(' ')
	text.WriteString(zoneToken(s.When))
	return text.String()
}

func parseZone(zone string) (int, error) {
	if !isZoneToken(zone) {
		return 0, fmt.Errorf("%w: bad timezone %q", ErrInvalidSignature, zone)
	}
	offset := (int(zone[1]-'0')*10+int(zone[2]-'0'))*3600 + (int(zone[3]-'0')*10+int(zone[4]-'0'))*60
	if zone[0] == '-' {
		return -offset, nil
	}
	return offset, nil
}

func isZoneToken(zone string) bool {
	if len(zone) != zoneTokenLength || (zone[0] != '+' && zone[0] != '-') {
		return false
	}
	for index := 1; index < zoneTokenLength; index++ {
		if zone[index] < '0' || zone[index] > '9' {
			return false
		}
	}
	return true
}

func zoneToken(when time.Time) string {
	if name := when.Location().String(); isZoneToken(name) {
		return name
	}
	_, offset := when.Zone()
	sign := byte('+')
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	minutes := offset / 60
	return fmt.Sprintf("%c%02d%02d", sign, minutes/60, minutes%60)
}
