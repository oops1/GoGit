package transport

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func fetchFeatures(value string) Capabilities {
	var caps Capabilities
	caps.Add("fetch=" + value)
	return caps
}

func readAllPack(t *testing.T, resp *FetchResponse) []byte {
	t.Helper()
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	return got
}

func TestFetchV1FlushesTheWantListBeforeTheFirstHave(t *testing.T) {
	haves := manyHaves(100, 60)
	pack := fakePack(61)
	rt := &scriptedRoundTripper{responses: [][]byte{
		newPktBuilder().line("ACK " + haves[3].String() + " common\n").line("ACK " + haves[7].String() + " common\n").line("NAK\n").bytes(),
		newPktBuilder().line("ACK " + haves[35].String() + " common\n").line("ACK " + haves[35].String() + " ready\n").line("NAK\n").bytes(),
		newPktBuilder().line("ACK " + haves[35].String() + "\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes(),
	}}
	neg := &sliceNegotiator{haves: haves}
	resp, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg, ParseCapabilities("side-band-64k multi_ack_detailed"), "gogit", false)
	if err != nil {
		t.Fatalf("fetchV1 returned error %v", err)
	}
	if got := readAllPack(t, resp); !bytes.Equal(got, pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
	if len(rt.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(rt.requests))
	}
	first := rt.requests[0]
	if flush, have := bytes.Index(first, []byte("0000")), bytes.Index(first, []byte("have ")); flush < 0 || have < 0 || flush > have {
		t.Fatalf("the want list was not flushed before the haves: %q", first)
	}
	if bytes.Contains(rt.requests[2], []byte("have ")) || !bytes.Contains(rt.requests[2], []byte("done\n")) {
		t.Fatalf("the request after ready = %q, want only done", rt.requests[2])
	}
	for _, id := range []hash.ObjectID{haves[3], haves[7], haves[35]} {
		if !slices.Contains(neg.common, id) {
			t.Fatalf("common = %v, missing %s", neg.common, id)
		}
	}
}

func TestFetchV1WithoutMultiAckStopsAtTheFirstAck(t *testing.T) {
	haves := manyHaves(100, 60)
	pack := fakePack(63)
	responses := func(finalAck bool) [][]byte {
		final := newPktBuilder()
		if finalAck {
			final.line("ACK " + haves[40].String() + "\n")
		}
		return [][]byte{
			newPktBuilder().line("NAK\n").bytes(),
			newPktBuilder().line("ACK " + haves[40].String() + "\n").bytes(),
			final.raw(sidebandFrame(SidebandPack, pack)).flush().bytes(),
		}
	}
	for _, stateless := range []bool{false, true} {
		rt := &scriptedRoundTripper{responses: responses(stateless)}
		resp, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, &sliceNegotiator{haves: haves}, ParseCapabilities("side-band-64k"), "gogit", stateless)
		if err != nil {
			t.Fatalf("stateless=%v: fetchV1 returned error %v", stateless, err)
		}
		if got := readAllPack(t, resp); !bytes.Equal(got, pack) {
			t.Fatalf("stateless=%v: Pack = %x, want %x", stateless, got, pack)
		}
		if !bytes.Contains(rt.requests[2], []byte("done\n")) {
			t.Fatalf("stateless=%v: the request after the ack = %q, want done", stateless, rt.requests[2])
		}
	}
}

func TestFetchV1ReadsTheShallowUpdateOfEveryStatelessRound(t *testing.T) {
	haves := manyHaves(40, 60)
	pack := fakePack(65)
	shallow := newPktBuilder().line("shallow " + idOf(2).String() + "\n").flush().bytes()
	rt := &scriptedRoundTripper{responses: [][]byte{
		append(slices.Clone(shallow), newPktBuilder().line("NAK\n").bytes()...),
		append(slices.Clone(shallow), newPktBuilder().line("ACK "+haves[0].String()+"\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes()...),
	}}
	resp, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}, Depth: 1}, &sliceNegotiator{haves: haves}, ParseCapabilities("side-band-64k multi_ack_detailed shallow"), "gogit", true)
	if err != nil {
		t.Fatalf("fetchV1 returned error %v", err)
	}
	if got := readAllPack(t, resp); !bytes.Equal(got, pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
	if len(resp.Shallow) != 1 || resp.Shallow[0] != idOf(2) {
		t.Fatalf("Shallow = %v", resp.Shallow)
	}
}

func TestFetchV1ReportsABrokenNegotiationRound(t *testing.T) {
	haves := manyHaves(100, 60)
	for name, body := range map[string][]byte{
		"flush instead of NAK": newPktBuilder().line("ACK " + haves[0].String() + " common\n").flush().bytes(),
		"cut short":            newPktBuilder().line("ACK " + haves[0].String() + " common\n").bytes(),
	} {
		rt := &scriptedRoundTripper{responses: [][]byte{body}}
		if _, err := fetchV1(t.Context(), rt, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, &sliceNegotiator{haves: haves}, ParseCapabilities("multi_ack_detailed"), "gogit", false); err == nil {
			t.Fatalf("%s: fetchV1 returned no error", name)
		}
	}
}

func TestFetchV2RefusesFeaturesTheServerDoesNotOffer(t *testing.T) {
	wants := []hash.ObjectID{idOf(1)}
	tests := []struct {
		name string
		req  FetchRequest
	}{
		{"filter", FetchRequest{Wants: wants, Filter: "blob:none"}},
		{"want-ref", FetchRequest{WantRefs: []string{"refs/heads/feature"}}},
		{"depth", FetchRequest{Wants: wants, Depth: 1}},
		{"shallow", FetchRequest{Wants: wants, Shallow: []hash.ObjectID{idOf(2)}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refused := &scriptedRoundTripper{}
			if _, err := fetchV2(t.Context(), refused, tc.req, nil, fetchFeatures("wait-for-done"), "gogit"); !errors.Is(err, ErrProtocol) || len(refused.requests) != 0 {
				t.Fatalf("fetchV2 returned %v after %d requests, want a refusal before sending", err, len(refused.requests))
			}
			offered := &scriptedRoundTripper{responses: [][]byte{
				newPktBuilder().line("packfile\n").raw(sidebandFrame(SidebandPack, fakePack(9))).flush().bytes(),
			}}
			resp, err := fetchV2(t.Context(), offered, tc.req, nil, fetchFeatures("shallow filter ref-in-want"), "gogit")
			if err != nil {
				t.Fatalf("fetchV2 returned error %v although the server offers the feature", err)
			}
			readAllPack(t, resp)
		})
	}
}
