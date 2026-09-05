package transport

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNegotiatedPushCapabilitiesAtomicAndPushOptions(t *testing.T) {
	caps := ParseCapabilities("report-status-v2 delete-refs atomic push-options ofs-delta side-band-64k")
	line, reportV2, usePushOptions := negotiatedPushCapabilities(caps, PushRequest{Atomic: true, Options: []string{"key=value"}}, "gogit")
	if !reportV2 {
		t.Fatalf("reportV2 = false, want true")
	}
	if !usePushOptions {
		t.Fatalf("usePushOptions = false, want true")
	}
	for _, want := range []string{CapReportStatusV2, CapDeleteRefs, CapAtomic, CapPushOptions, CapOfsDelta, CapSideBand64k, "agent=gogit"} {
		if !strings.Contains(line, want) {
			t.Fatalf("capability line %q missing %q", line, want)
		}
	}
}

func TestNegotiatedPushCapabilitiesFallsBackToReportStatus(t *testing.T) {
	caps := ParseCapabilities("report-status side-band")
	line, reportV2, usePushOptions := negotiatedPushCapabilities(caps, PushRequest{}, "gogit")
	if reportV2 {
		t.Fatalf("reportV2 = true, want false")
	}
	if usePushOptions {
		t.Fatalf("usePushOptions = true, want false (no push-options capability)")
	}
	if !strings.Contains(line, CapReportStatus) || strings.Contains(line, CapReportStatusV2) {
		t.Fatalf("capability line %q, want report-status without v2", line)
	}
	if !strings.Contains(line, CapSideBand) {
		t.Fatalf("capability line %q missing side-band", line)
	}
}

func TestNegotiatedPushCapabilitiesIgnoresAtomicAndOptionsWithoutServerSupport(t *testing.T) {
	caps := ParseCapabilities("report-status")
	_, _, usePushOptions := negotiatedPushCapabilities(caps, PushRequest{Atomic: true, Options: []string{"x"}}, "gogit")
	if usePushOptions {
		t.Fatalf("usePushOptions = true, want false when the server lacks push-options")
	}
}

func TestBuildPushRequestIncludesPushOptionLines(t *testing.T) {
	caps := ParseCapabilities("report-status push-options")
	body, _ := buildPushRequest(PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Options: []string{"issue=123"},
	}, caps, "gogit")
	if !bytes.Contains(body, []byte("issue=123")) {
		t.Fatalf("body = %q, missing push-option", body)
	}
}

func TestFinishPushWithoutSideband(t *testing.T) {
	body := reportStatusBody("ok", []string{"ok refs/heads/main"})
	result, err := finishPush(bytes.NewReader(body), Capabilities{})
	if err != nil {
		t.Fatalf("finishPush returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("UnpackOK = false")
	}
}

func TestFinishPushWithSideband(t *testing.T) {
	inner := reportStatusBody("ok", []string{"ok refs/heads/main"})
	framed := buildSidebandPack(inner)
	caps := ParseCapabilities("side-band-64k")
	result, err := finishPush(bytes.NewReader(framed), caps)
	if err != nil {
		t.Fatalf("finishPush returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("UnpackOK = false")
	}
}

func TestParseReportStatusUnpackError(t *testing.T) {
	body := reportStatusBody("error: failed to update refs", nil)
	result, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseReportStatus returned error %v", err)
	}
	if result.UnpackOK {
		t.Fatalf("UnpackOK = true, want false")
	}
	if result.UnpackError != "error: failed to update refs" {
		t.Fatalf("UnpackError = %q", result.UnpackError)
	}
}

func TestParseReportStatusPropagatesReadErrorOnUnpackLine(t *testing.T) {
	_, err := parseReportStatus(NewDecoder(bytes.NewReader([]byte("000"))))
	if err == nil {
		t.Fatalf("parseReportStatus succeeded on a truncated stream")
	}
}

func TestParseReportStatusPropagatesReadErrorAfterUnpackLine(t *testing.T) {
	body := newPktBuilder().line("unpack ok\n").bytes()
	body = append(body, []byte("000")...)
	_, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if err == nil {
		t.Fatalf("parseReportStatus succeeded on a stream truncated after the unpack line")
	}
}

func TestParseReportStatusSkipsNonDataPackets(t *testing.T) {
	body := newPktBuilder().line("unpack ok\n").delim().line("ok refs/heads/main\n").flush().bytes()
	result, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseReportStatus returned error %v", err)
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("Refs = %+v", result.Refs)
	}
}

func TestParseReportStatusMissingUnpackLine(t *testing.T) {
	body := newPktBuilder().flush().bytes()
	_, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseReportStatus returned %v, want ErrProtocol", err)
	}
}

func TestParseReportStatusMalformedUnpackLine(t *testing.T) {
	body := newPktBuilder().line("bogus\n").flush().bytes()
	_, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseReportStatus returned %v, want ErrProtocol", err)
	}
}

func TestParseReportStatusNgWithoutMessage(t *testing.T) {
	body := reportStatusBody("ok", []string{"ng refs/heads/main"})
	result, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseReportStatus returned error %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].OK || result.Refs[0].Name != "refs/heads/main" || result.Refs[0].Message != "" {
		t.Fatalf("Refs = %+v", result.Refs)
	}
}

func TestParseReportStatusOptionWithoutPrecedingRef(t *testing.T) {
	body := reportStatusBody("ok", []string{"option refname refs/heads/main"})
	_, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseReportStatus returned %v, want ErrProtocol", err)
	}
}

func TestParseReportStatusUnexpectedLine(t *testing.T) {
	body := reportStatusBody("ok", []string{"bogus line"})
	_, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseReportStatus returned %v, want ErrProtocol", err)
	}
}

func TestParseReportStatusAccumulatesMultipleOptionLines(t *testing.T) {
	body := reportStatusBody("ok", []string{
		"ok refs/heads/main",
		"option refname refs/heads/main",
		"option new-oid " + idOf(2).String(),
	})
	result, err := parseReportStatus(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseReportStatus returned error %v", err)
	}
	want := "refname refs/heads/main; new-oid " + idOf(2).String()
	if result.Refs[0].Message != want {
		t.Fatalf("Message = %q, want %q", result.Refs[0].Message, want)
	}
}
