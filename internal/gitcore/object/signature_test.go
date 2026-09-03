package object_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestParseSignatureRoundTripsRealAndUnusualLines(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		author  string
		email   string
		unix    int64
		offset  int
		noZone  bool
		zoneKey string
	}{
		{
			name:    "positive offset",
			line:    "A U Thor <author@example.com> 1700000000 +0300",
			author:  "A U Thor",
			email:   "author@example.com",
			unix:    1700000000,
			offset:  3 * 3600,
			zoneKey: "+0300",
		},
		{
			name:    "negative offset with minutes",
			line:    "C O Mitter <committer@example.com> 1700000100 -0430",
			author:  "C O Mitter",
			email:   "committer@example.com",
			unix:    1700000100,
			offset:  -(4*3600 + 30*60),
			zoneKey: "-0430",
		},
		{
			name:    "negative zero means unknown zone",
			line:    "A  Double  Space <ds@example.com> 1700000000 -0000",
			author:  "A  Double  Space",
			email:   "ds@example.com",
			unix:    1700000000,
			offset:  0,
			zoneKey: "-0000",
		},
		{
			name:    "offset beyond twelve hours",
			line:    " <empty@example.com> 1465555555 +1345",
			author:  "",
			email:   "empty@example.com",
			unix:    1465555555,
			offset:  13*3600 + 45*60,
			zoneKey: "+1345",
		},
		{
			name:   "timezone omitted altogether",
			line:   "No Zone <nozone@example.com> 1700000000",
			author: "No Zone",
			email:  "nozone@example.com",
			unix:   1700000000,
			noZone: true,
		},
		{
			name:    "negative timestamp before the epoch",
			line:    "Old Timer <old@example.com> -86400 +0000",
			author:  "Old Timer",
			email:   "old@example.com",
			unix:    -86400,
			offset:  0,
			zoneKey: "+0000",
		},
		{
			name:    "angle brackets inside the name",
			line:    "Nick <nick> <nick@example.com> 1700000000 +0200",
			author:  "Nick <nick>",
			email:   "nick@example.com",
			unix:    1700000000,
			offset:  2 * 3600,
			zoneKey: "+0200",
		},
		{
			name:    "empty email",
			line:    "Nobody <> 0 +0000",
			author:  "Nobody",
			email:   "",
			unix:    0,
			offset:  0,
			zoneKey: "+0000",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			signature, err := object.ParseSignature([]byte(c.line))
			if err != nil {
				t.Fatalf("ParseSignature: %v", err)
			}
			if signature.Name != c.author || signature.Email != c.email {
				t.Fatalf("identity = %q <%q>", signature.Name, signature.Email)
			}
			if signature.When.Unix() != c.unix {
				t.Fatalf("Unix() = %d, want %d", signature.When.Unix(), c.unix)
			}
			if signature.OmitZone != c.noZone {
				t.Fatalf("OmitZone = %v, want %v", signature.OmitZone, c.noZone)
			}
			if name, offset := signature.When.Zone(); !c.noZone && (offset != c.offset || name != c.zoneKey) {
				t.Fatalf("Zone() = %q, %d; want %q, %d", name, offset, c.zoneKey, c.offset)
			}
			if got := signature.String(); got != c.line {
				t.Fatalf("String() = %q, want %q", got, c.line)
			}
		})
	}
}

func TestParseSignatureRejectsBrokenLines(t *testing.T) {
	cases := map[string]string{
		"no email":                    "A U Thor 1700000000 +0300",
		"no space before":             "AUThor<author@example.com> 1700000000 +0300",
		"email at the start":          "<author@example.com> 1700000000 +0300",
		"unclosed email":              "A U Thor <author@example.com 1700000000 +0300",
		"nothing after email":         "A U Thor <author@example.com>",
		"no space after":              "A U Thor <author@example.com>1700000000",
		"timestamp not a number":      "A U Thor <author@example.com> yesterday +0300",
		"timestamp with leading zero": "A U Thor <author@example.com> 01700000000 +0300",
		"timestamp with a plus sign":  "A U Thor <author@example.com> +1700000000 +0300",
		"empty timestamp":             "A U Thor <author@example.com>  +0300",
		"three trailing fields":       "A U Thor <author@example.com> 1700000000 +0300 extra",
		"short zone":                  "A U Thor <author@example.com> 1700000000 +03",
		"zone without sign":           "A U Thor <author@example.com> 1700000000 03000",
		"zone with letters":           "A U Thor <author@example.com> 1700000000 +03x0",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := object.ParseSignature([]byte(line)); !errors.Is(err, object.ErrInvalidSignature) {
				t.Fatalf("err = %v, want ErrInvalidSignature", err)
			}
		})
	}
}

func TestSignatureStringDerivesZoneFromOffsetWhenLocationIsNotAToken(t *testing.T) {
	cases := []struct {
		name     string
		location *time.Location
		want     string
	}{
		{"utc", time.UTC, "A U Thor <a@example.com> 1700000000 +0000"},
		{"named east", time.FixedZone("MSK", 3*3600), "A U Thor <a@example.com> 1700000000 +0300"},
		{"named west", time.FixedZone("PST", -(8*3600 + 30*60)), "A U Thor <a@example.com> 1700000000 -0830"},
		{"token shaped but not digits", time.FixedZone("+03x0", 3*3600), "A U Thor <a@example.com> 1700000000 +0300"},
		{"token shaped but too long", time.FixedZone("+03000", 3*3600), "A U Thor <a@example.com> 1700000000 +0300"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			signature := object.Signature{
				Name:  "A U Thor",
				Email: "a@example.com",
				When:  time.Unix(1700000000, 0).In(c.location),
			}
			if got := signature.String(); got != c.want {
				t.Fatalf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSignatureStringOmitsZoneOnRequest(t *testing.T) {
	signature := object.Signature{
		Name:     "No Zone",
		Email:    "nozone@example.com",
		When:     time.Unix(1700000000, 0).UTC(),
		OmitZone: true,
	}
	if got := signature.String(); got != "No Zone <nozone@example.com> 1700000000" {
		t.Fatalf("String() = %q", got)
	}
}
