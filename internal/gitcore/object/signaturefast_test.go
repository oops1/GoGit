package object

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func referenceSeconds(stamp string) (int64, bool) {
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != stamp {
		return 0, false
	}
	return seconds, true
}

func TestCanonicalSecondsAgreeWithParsingAndFormattingAnInteger(t *testing.T) {
	stamps := []string{
		"", "0", "-0", "+1", "01", "-", "--1", "-01", "1", "-1", "1700000000", "-1700000000",
		"9223372036854775807", "9223372036854775808", "-9223372036854775808", "-9223372036854775809",
		"99999999999999999999", "12a", "1_0", " 1", "1 ", "0x10", "1e3", "٣",
	}
	for _, stamp := range stamps {
		wantSeconds, wantOK := referenceSeconds(stamp)
		gotSeconds, gotOK := parseCanonicalSeconds(stamp)
		if gotOK != wantOK || (gotOK && gotSeconds != wantSeconds) {
			t.Errorf("parseCanonicalSeconds(%q) = %d, %v; parsing and formatting gives %d, %v", stamp, gotSeconds, gotOK, wantSeconds, wantOK)
		}
	}
}

func TestInvalidTimestampsKeepTheirFailureMessages(t *testing.T) {
	tests := []struct {
		stamp string
		want  string
	}{
		{"12a", "invalid syntax"},
		{"99999999999999999999", "value out of range"},
		{"01", "is not canonical"},
		{"+1", "is not canonical"},
		{"-0", "is not canonical"},
	}
	for _, tc := range tests {
		err := invalidTimestamp(tc.stamp)
		if !errors.Is(err, ErrInvalidSignature) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("invalidTimestamp(%q) = %v, want %q", tc.stamp, err, tc.want)
		}
	}
}

func TestParsingASignatureRejectsATimestampThatIsNotCanonical(t *testing.T) {
	_, err := ParseSignature([]byte("Ann <ann@example.com> 0100 +0300"))

	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("ParseSignature returned %v, want ErrInvalidSignature", err)
	}
}

func TestSignaturesInTheSameZoneShareOneLocation(t *testing.T) {
	emptyZoneCache(t)
	first, err := ParseSignature([]byte("Ann <ann@example.com> 1700000000 +0530"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseSignature([]byte("Bob <bob@example.com> 1700000100 +0530"))
	if err != nil {
		t.Fatal(err)
	}

	if first.When.Location() != second.When.Location() {
		t.Fatal("two signatures in +0530 got different location objects")
	}
	if name := first.When.Location().String(); name != "+0530" {
		t.Fatalf("the location is named %q, want +0530", name)
	}
	if _, offset := first.When.Zone(); offset != 5*3600+30*60 {
		t.Fatalf("the offset is %d, want +0530", offset)
	}
}

func TestTheZoneCacheStopsGrowingAtItsLimit(t *testing.T) {
	emptyZoneCache(t)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for index := range zoneCacheLimit + 500 {
				token := fmt.Sprintf("+%04d", (index*4+worker)%10000)
				offset, err := parseZone(token)
				if err != nil {
					t.Errorf("parseZone(%q) returned error %v", token, err)
					return
				}
				location := zoneLocation(token, offset)
				if location.String() != token {
					t.Errorf("zoneLocation(%q) is named %q", token, location.String())
					return
				}
			}
		})
	}
	wg.Wait()

	zoneCache.RLock()
	size := len(zoneCache.locations)
	zoneCache.RUnlock()
	if size > zoneCacheLimit {
		t.Fatalf("the zone cache holds %d locations, want at most %d", size, zoneCacheLimit)
	}
	beyond, err := parseZone("-9999")
	if err != nil {
		t.Fatal(err)
	}
	if fresh := zoneLocation("-9999", beyond); fresh == nil || fresh.String() != "-9999" {
		t.Fatalf("a zone beyond the limit returned %v", fresh)
	}
}

func emptyZoneCache(t *testing.T) {
	t.Helper()
	zoneCache.Lock()
	saved := zoneCache.locations
	zoneCache.locations = make(map[string]*time.Location)
	zoneCache.Unlock()
	t.Cleanup(func() {
		zoneCache.Lock()
		zoneCache.locations = saved
		zoneCache.Unlock()
	})
}

func TestParsingACommitAllocatesLittle(t *testing.T) {
	emptyZoneCache(t)
	data := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
		"parent 1111111111111111111111111111111111111111\n" +
		"author Ann <ann@example.com> 1700000000 +0300\n" +
		"committer Bob <bob@example.com> 1700000100 +0300\n\nmessage\n")

	allocations := testing.AllocsPerRun(100, func() {
		if _, err := ParseCommit(data); err != nil {
			t.Fatal(err)
		}
	})

	if allocations > 10 {
		t.Fatalf("ParseCommit allocates %v times, want at most 10", allocations)
	}
}
