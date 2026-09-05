package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

type erroringNegotiator struct{ err error }

func (n *erroringNegotiator) Haves(context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		yield(hash.Zero, n.err)
	}
}

func (n *erroringNegotiator) Common(hash.ObjectID) {}

func (n *erroringNegotiator) Enough() bool { return false }

type scriptedRoundTripper struct {
	responses [][]byte
	requests  [][]byte
	i         int
	err       error
}

func (s *scriptedRoundTripper) round(_ context.Context, body []byte) (io.ReadCloser, error) {
	s.requests = append(s.requests, append([]byte(nil), body...))
	if s.err != nil {
		return nil, s.err
	}
	if s.i >= len(s.responses) {
		return nil, errors.New("scriptedRoundTripper: no more scripted responses")
	}
	resp := s.responses[s.i]
	s.i++
	return io.NopCloser(bytes.NewReader(resp)), nil
}

func manyHaves(n int, offset byte) []hash.ObjectID {
	haves := make([]hash.ObjectID, n)
	for i := range haves {
		haves[i] = idOf(offset + byte(i))
	}
	return haves
}

func fetchV2AckLinesBody(lines []string) []byte {
	b := newPktBuilder().line("acknowledgments\n")
	for _, l := range lines {
		b.line(l + "\n")
	}
	return b.flush().bytes()
}

func TestFetchV2StopsNegotiatingAfterReadyAck(t *testing.T) {
	haves := manyHaves(100, 20)
	pack := fakePack(23)
	rt := &scriptedRoundTripper{responses: [][]byte{
		fetchV2AckLinesBody([]string{"NAK"}),
		fetchV2AckLinesBody([]string{"ready"}),
		fetchV2PackfileBody(pack),
	}}
	neg := &sliceNegotiator{haves: haves}
	resp, err := fetchV2(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg, Capabilities{}, "gogit")
	if err != nil {
		t.Fatalf("fetchV2 returned error %v", err)
	}
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
	if len(rt.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(rt.requests))
	}
	if got := bytes.Count(rt.requests[2], []byte("have ")); got != 64 {
		t.Fatalf("the final request carries %d have lines, want 64 (no new haves sent after ready)", got)
	}
	if !bytes.Contains(rt.requests[2], []byte("done\n")) {
		t.Fatalf("the final request is missing done: %q", rt.requests[2])
	}
}

func TestFetchV2ReturnsErrorWhenServerNeverSendsAPackfile(t *testing.T) {
	rt := &scriptedRoundTripper{responses: [][]byte{fetchV2AckLinesBody([]string{"NAK"})}}
	_, err := fetchV2(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit")
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV2 returned %v, want ErrProtocol", err)
	}
}

func TestFetchV2PropagatesRoundTripperError(t *testing.T) {
	boom := errors.New("boom")
	rt := &scriptedRoundTripper{err: boom}
	_, err := fetchV2(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit")
	if !errors.Is(err, boom) {
		t.Fatalf("fetchV2 returned %v, want boom", err)
	}
}

func TestFetchV2RejectsEmptyRequest(t *testing.T) {
	rt := &scriptedRoundTripper{}
	_, err := fetchV2(t.Context(), rt, FetchRequest{}, nil, Capabilities{}, "gogit")
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV2 returned %v, want ErrProtocol", err)
	}
}

func TestFetchV2ShallowInfoAndWantedRefsSections(t *testing.T) {
	pack := fakePack(29)
	body := newPktBuilder().
		line("shallow-info\n").
		line("shallow " + idOf(5).String() + "\n").
		line("unshallow " + idOf(6).String() + "\n").
		line("wanted-refs\n").
		line(idOf(7).String() + " refs/heads/feature\n").
		line("packfile\n").
		raw(sidebandFrame(SidebandPack, pack)).
		flush().bytes()
	rt := &scriptedRoundTripper{responses: [][]byte{body}}
	resp, err := fetchV2(t.Context(), rt, FetchRequest{
		Wants:    []hash.ObjectID{idOf(1)},
		WantRefs: []string{"refs/heads/feature"},
		Depth:    1,
	}, nil, Capabilities{}, "gogit")
	if err != nil {
		t.Fatalf("fetchV2 returned error %v", err)
	}
	if len(resp.Shallow) != 1 || resp.Shallow[0] != idOf(5) {
		t.Fatalf("Shallow = %v", resp.Shallow)
	}
	if len(resp.Unshallow) != 1 || resp.Unshallow[0] != idOf(6) {
		t.Fatalf("Unshallow = %v", resp.Unshallow)
	}
	if len(resp.WantedRefs) != 1 || resp.WantedRefs[0].Name != "refs/heads/feature" || resp.WantedRefs[0].ID != idOf(7) {
		t.Fatalf("WantedRefs = %v", resp.WantedRefs)
	}
	if !bytes.Contains(rt.requests[0], []byte("deepen 1\n")) {
		t.Fatalf("request %q missing deepen 1", rt.requests[0])
	}
	if !bytes.Contains(rt.requests[0], []byte("want-ref refs/heads/feature\n")) {
		t.Fatalf("request %q missing want-ref", rt.requests[0])
	}
}

func TestParseFetchV2RoundRejectsLineOutsideAnySection(t *testing.T) {
	body := newPktBuilder().line("bogus\n").flush().bytes()
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseFetchV2Round returned %v, want ErrProtocol", err)
	}
}

func TestParseFetchV2RoundRejectsMalformedShallowInfoLine(t *testing.T) {
	body := newPktBuilder().line("shallow-info\n").line("bogus\n").flush().bytes()
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseFetchV2Round returned %v, want ErrProtocol", err)
	}
}

func TestParseFetchV2RoundRejectsMalformedShallowOid(t *testing.T) {
	body := newPktBuilder().line("shallow-info\n").line("shallow not-an-oid\n").flush().bytes()
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if err == nil {
		t.Fatalf("parseFetchV2Round succeeded on a malformed shallow oid")
	}
	body2 := newPktBuilder().line("shallow-info\n").line("unshallow not-an-oid\n").flush().bytes()
	_, _, err = parseFetchV2Round(NewDecoder(bytes.NewReader(body2)))
	if err == nil {
		t.Fatalf("parseFetchV2Round succeeded on a malformed unshallow oid")
	}
}

func TestParseFetchV2RoundRejectsMalformedWantedRefLine(t *testing.T) {
	body := newPktBuilder().line("wanted-refs\n").line("bogus\n").flush().bytes()
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseFetchV2Round returned %v, want ErrProtocol", err)
	}
	body2 := newPktBuilder().line("wanted-refs\n").line("not-an-oid refs/heads/x\n").flush().bytes()
	_, _, err = parseFetchV2Round(NewDecoder(bytes.NewReader(body2)))
	if err == nil {
		t.Fatalf("parseFetchV2Round succeeded on a malformed wanted-ref oid")
	}
}

func TestParseFetchV2RoundRejectsMalformedAckLine(t *testing.T) {
	body := newPktBuilder().line("acknowledgments\n").line("bogus\n").flush().bytes()
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("parseFetchV2Round returned %v, want ErrProtocol", err)
	}
}

func TestHasReadyAckDetectsReady(t *testing.T) {
	if hasReadyAck([]ackLine{{status: ackNAK}}) {
		t.Fatalf("hasReadyAck = true, want false")
	}
	if !hasReadyAck([]ackLine{{status: ackNAK}, {status: ackReady}}) {
		t.Fatalf("hasReadyAck = false, want true")
	}
}

func TestFetchV1RejectsEmptyWants(t *testing.T) {
	rt := &scriptedRoundTripper{}
	_, err := fetchV1(t.Context(), rt, FetchRequest{}, nil, Capabilities{}, "gogit", false)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV1 returned %v, want ErrProtocol", err)
	}
}

func TestFetchV1PropagatesRoundTripperError(t *testing.T) {
	boom := errors.New("boom")
	rt := &scriptedRoundTripper{err: boom}
	_, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit", false)
	if !errors.Is(err, boom) {
		t.Fatalf("fetchV1 returned %v, want boom", err)
	}
}

func TestFetchV1RejectsMalformedAckLine(t *testing.T) {
	rt := &scriptedRoundTripper{responses: [][]byte{newPktBuilder().line("bogus\n").bytes()}}
	_, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit", false)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV1 returned %v, want ErrProtocol", err)
	}
}

func TestFetchV1RejectsFlushInsteadOfAck(t *testing.T) {
	rt := &scriptedRoundTripper{responses: [][]byte{newPktBuilder().flush().bytes()}}
	_, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit", false)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV1 returned %v, want ErrProtocol", err)
	}
}

func TestFetchV1WithDeepenReadsShallowUpdate(t *testing.T) {
	pack := fakePack(31)
	resp1 := newPktBuilder().
		line("shallow " + idOf(2).String() + "\n").
		flush().
		line("NAK\n").
		raw(sidebandFrame(SidebandPack, pack)).
		flush().bytes()
	rt := &scriptedRoundTripper{responses: [][]byte{resp1}}
	caps := ParseCapabilities("side-band-64k")
	resp, err := fetchV1(t.Context(), rt, FetchRequest{
		Wants: []hash.ObjectID{idOf(1)},
		Depth: 1,
	}, nil, caps, "gogit", false)
	if err != nil {
		t.Fatalf("fetchV1 returned error %v", err)
	}
	if len(resp.Shallow) != 1 || resp.Shallow[0] != idOf(2) {
		t.Fatalf("Shallow = %v", resp.Shallow)
	}
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestFetchV1StatelessResendsWantAndHavesEveryRound(t *testing.T) {
	haves := manyHaves(40, 50)
	pack := fakePack(37)
	rt := &scriptedRoundTripper{responses: [][]byte{
		newPktBuilder().line("NAK\n").bytes(),
		newPktBuilder().line("ACK " + haves[0].String() + "\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes(),
	}}
	caps := ParseCapabilities("side-band-64k multi_ack_detailed")
	neg := &sliceNegotiator{haves: haves}
	resp, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg, caps, "gogit", true)
	if err != nil {
		t.Fatalf("fetchV1 returned error %v", err)
	}
	if _, err := io.ReadAll(resp.Pack); err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if len(rt.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(rt.requests))
	}
	if bytes.Count(rt.requests[1], []byte("have ")) != 40 {
		t.Fatalf("second request should resend all 40 haves, body: %q", rt.requests[1])
	}
	if !bytes.Contains(rt.requests[1], []byte("want "+idOf(1).String())) {
		t.Fatalf("second request should resend the want line, body: %q", rt.requests[1])
	}
}

func TestNegotiatedV1CapabilitiesVariants(t *testing.T) {
	caps := ParseCapabilities("multi_ack_detailed side-band thin-pack ofs-delta shallow deepen-since deepen-not include-tag filter")
	line := negotiatedV1Capabilities(caps, FetchRequest{
		ThinPack:    true,
		IncludeTags: true,
		Filter:      "blob:none",
		Shallow:     []hash.ObjectID{idOf(9)},
		Progress:    func(progress.Report) {},
	}, "gogit")
	tokens := strings.Fields(line)
	for _, want := range []string{CapMultiAckDetailed, CapSideBand, CapThinPack, CapOfsDelta, CapShallow, CapIncludeTag, CapFilter, "agent=gogit"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("capability line %q missing %q", line, want)
		}
	}
	if slices.Contains(tokens, CapNoProgress) {
		t.Fatalf("capability line %q should not include no-progress when Progress is set", line)
	}
	if slices.Contains(tokens, CapSideBand64k) {
		t.Fatalf("capability line %q should prefer side-band over side-band-64k when only side-band is advertised", line)
	}
}

func TestNegotiatedV1CapabilitiesDeepenNotWithoutDeepenSince(t *testing.T) {
	caps := ParseCapabilities("shallow deepen-since deepen-not")
	line := negotiatedV1Capabilities(caps, FetchRequest{DeepenNot: []string{"refs/heads/old"}}, "gogit")
	tokens := strings.Fields(line)
	if !slices.Contains(tokens, CapDeepenNot) {
		t.Fatalf("capability line %q missing deepen-not", line)
	}
	if slices.Contains(tokens, CapDeepenSince) {
		t.Fatalf("capability line %q should not include deepen-since", line)
	}
}

func TestBuildFetchV2RequestAllOptions(t *testing.T) {
	body := buildFetchV2Request(FetchRequest{
		Wants:       []hash.ObjectID{idOf(1)},
		WantRefs:    []string{"refs/heads/feature"},
		ThinPack:    true,
		IncludeTags: true,
		Filter:      "blob:none",
		Depth:       2,
		Progress:    func(progress.Report) {},
	}, "gogit", nil, false)
	text := string(body)
	for _, want := range []string{"thin-pack\n", "include-tag\n", "filter blob:none\n", "want-ref refs/heads/feature\n", "deepen 2\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body %q missing %q", text, want)
		}
	}
	if strings.Contains(text, "no-progress\n") {
		t.Fatalf("body %q should not include no-progress when Progress is set", text)
	}
	if strings.Contains(text, "done\n") {
		t.Fatalf("body %q should not include done for a non-final round", text)
	}
}

func TestFetchV1PropagatesNegotiatorHaveError(t *testing.T) {
	boom := errors.New("boom")
	rt := &scriptedRoundTripper{}
	neg := &erroringNegotiator{err: boom}
	_, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg, Capabilities{}, "gogit", false)
	if !errors.Is(err, boom) {
		t.Fatalf("fetchV1 returned %v, want boom", err)
	}
}

func TestFetchV1FailsWhenShallowUpdateIsMalformed(t *testing.T) {
	body := newPktBuilder().line("bogus\n").flush().bytes()
	rt := &scriptedRoundTripper{responses: [][]byte{body}}
	_, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}, Depth: 1}, nil, Capabilities{}, "gogit", false)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("fetchV1 returned %v, want ErrProtocol", err)
	}
}

func TestBuildV1WantPrefixIncludesShallowAndMultipleWants(t *testing.T) {
	body := buildV1WantPrefix(FetchRequest{
		Wants:   []hash.ObjectID{idOf(1), idOf(2)},
		Shallow: []hash.ObjectID{idOf(3)},
	}, Capabilities{}, "gogit")
	text := string(body)
	if !strings.Contains(text, "want "+idOf(2).String()+"\n") {
		t.Fatalf("body %q missing second want line", text)
	}
	if !strings.Contains(text, "shallow "+idOf(3).String()+"\n") {
		t.Fatalf("body %q missing shallow line", text)
	}
}

func TestNegotiatedV1CapabilitiesDeepenSinceIncluded(t *testing.T) {
	caps := ParseCapabilities("shallow deepen-since")
	line := negotiatedV1Capabilities(caps, FetchRequest{DeepenSince: time.Unix(1000, 0)}, "gogit")
	if !slices.Contains(strings.Fields(line), CapDeepenSince) {
		t.Fatalf("capability line %q missing deepen-since", line)
	}
}

func TestFetchV2PropagatesNegotiatorHaveError(t *testing.T) {
	boom := errors.New("boom")
	rt := &scriptedRoundTripper{}
	neg := &erroringNegotiator{err: boom}
	_, err := fetchV2(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg, Capabilities{}, "gogit")
	if !errors.Is(err, boom) {
		t.Fatalf("fetchV2 returned %v, want boom", err)
	}
}

func TestFetchV2PropagatesMalformedRoundError(t *testing.T) {
	rt := &scriptedRoundTripper{responses: [][]byte{[]byte("000")}}
	_, err := fetchV2(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil, Capabilities{}, "gogit")
	if err == nil {
		t.Fatalf("fetchV2 succeeded on a malformed response")
	}
}

func TestBuildFetchV2RequestIncludesShallowLines(t *testing.T) {
	body := buildFetchV2Request(FetchRequest{
		Wants:   []hash.ObjectID{idOf(1)},
		Shallow: []hash.ObjectID{idOf(2)},
	}, "gogit", nil, false)
	if !strings.Contains(string(body), "shallow "+idOf(2).String()+"\n") {
		t.Fatalf("body %q missing shallow line", body)
	}
}

func TestParseFetchV2RoundPropagatesReadError(t *testing.T) {
	_, _, err := parseFetchV2Round(NewDecoder(bytes.NewReader([]byte("000"))))
	if err == nil {
		t.Fatalf("parseFetchV2Round succeeded on a truncated stream")
	}
}

func TestParseFetchV2RoundSkipsNonDataPackets(t *testing.T) {
	body := newPktBuilder().delim().line("acknowledgments\n").line("NAK\n").flush().bytes()
	round, sawPackfile, err := parseFetchV2Round(NewDecoder(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("parseFetchV2Round returned error %v", err)
	}
	if sawPackfile {
		t.Fatalf("sawPackfile = true, want false")
	}
	if len(round.acks) != 1 || round.acks[0].status != ackNAK {
		t.Fatalf("acks = %v", round.acks)
	}
}

func TestNegHavesOnNilNegotiatorYieldsNothing(t *testing.T) {
	count := 0
	for range negHaves(t.Context(), nil) {
		count++
	}
	if count != 0 {
		t.Fatalf("negHaves(nil) yielded %d items, want 0", count)
	}
}
