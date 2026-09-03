package object

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
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
	open := bytes.LastIndexByte(line, '<')
	if open < 1 || line[open-1] != ' ' {
		return Signature{}, fmt.Errorf("%w: no name before email in %q", ErrInvalidSignature, line)
	}
	closing := bytes.IndexByte(line[open:], '>')
	if closing < 0 {
		return Signature{}, fmt.Errorf("%w: email is not closed in %q", ErrInvalidSignature, line)
	}
	closing += open
	rest := line[closing+1:]
	if len(rest) == 0 || rest[0] != ' ' {
		return Signature{}, fmt.Errorf("%w: no timestamp in %q", ErrInvalidSignature, line)
	}
	stamp, zone, hasZone := bytes.Cut(rest[1:], []byte(" "))
	seconds, err := strconv.ParseInt(string(stamp), 10, 64)
	if err != nil {
		return Signature{}, fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	if strconv.FormatInt(seconds, 10) != string(stamp) {
		return Signature{}, fmt.Errorf("%w: timestamp %q is not canonical", ErrInvalidSignature, stamp)
	}
	signature := Signature{
		Name:     string(line[:open-1]),
		Email:    string(line[open+1 : closing]),
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
	signature.When = time.Unix(seconds, 0).In(time.FixedZone(string(zone), offset))
	return signature, nil
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

func parseZone(zone []byte) (int, error) {
	if !isZoneToken(string(zone)) {
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
