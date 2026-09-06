package transport

import (
	"context"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func negotiatedV1Capabilities(caps Capabilities, req FetchRequest, agent string) string {
	var tokens []string
	add := func(name string) {
		if caps.Has(name) {
			tokens = append(tokens, name)
		}
	}
	add(CapMultiAckDetailed)
	switch {
	case caps.Has(CapSideBand64k):
		tokens = append(tokens, CapSideBand64k)
	case caps.Has(CapSideBand):
		tokens = append(tokens, CapSideBand)
	}
	add(CapOfsDelta)
	if req.ThinPack {
		add(CapThinPack)
	}
	if hasDeepenArgs(req) || len(req.Shallow) > 0 {
		add(CapShallow)
		if !req.DeepenSince.IsZero() {
			add(CapDeepenSince)
		}
		if len(req.DeepenNot) > 0 {
			add(CapDeepenNot)
		}
	}
	if req.IncludeTags {
		add(CapIncludeTag)
	}
	if req.Filter != "" {
		add(CapFilter)
	}
	if req.Progress == nil {
		add(CapNoProgress)
	}
	tokens = append(tokens, CapAgent+"="+agent)
	return strings.Join(tokens, " ")
}

func buildV1WantPrefix(req FetchRequest, caps Capabilities, agent string) []byte {
	var body []byte
	capsLine := negotiatedV1Capabilities(caps, req, agent)
	for i, id := range req.Wants {
		if i == 0 {
			body = appendPktLine(body, "want "+id.String()+" "+capsLine+"\n")
			continue
		}
		body = appendPktLine(body, "want "+id.String()+"\n")
	}
	for _, id := range req.Shallow {
		body = appendPktLine(body, "shallow "+id.String()+"\n")
	}
	body = appendDeepenArgs(body, req, appendPktLine)
	return body
}

func appendHaveLines(body []byte, ids []hash.ObjectID) []byte {
	for _, id := range ids {
		body = appendPktLine(body, "have "+id.String()+"\n")
	}
	return body
}

func fetchV1(ctx context.Context, rt roundTripper, req FetchRequest, neg Negotiator, caps Capabilities, agent string, stateless bool) (*FetchResponse, error) {
	if len(req.Wants) == 0 {
		return nil, fmt.Errorf("%w: fetch request has no wants", ErrProtocol)
	}
	prefix := buildV1WantPrefix(req, caps, agent)
	haveNext := haveIterFunc(negHaves(ctx, neg))
	var sentHaves []hash.ObjectID
	resp := &FetchResponse{}
	forceFinal := false
	round := 0
	for {
		round++
		var batch []hash.ObjectID
		exhausted := forceFinal
		var err error
		if !forceFinal {
			batch, exhausted, err = pullHaves(haveNext, maxHavesPerRound)
			if err != nil {
				return nil, err
			}
		}
		final := forceFinal || exhausted || (neg != nil && neg.Enough())

		var body []byte
		if stateless || round == 1 {
			body = append(body, prefix...)
		}
		if stateless {
			body = appendHaveLines(body, sentHaves)
		}
		body = appendHaveLines(body, batch)
		sentHaves = append(sentHaves, batch...)
		if final {
			body = appendDonePkt(body)
		} else {
			body = appendFlushPkt(body)
		}

		reader, err := rt.round(ctx, body)
		if err != nil {
			return nil, err
		}
		dec := NewDecoder(reader)
		if round == 1 && hasDeepenArgs(req) {
			shallow, unshallow, err := readShallowUpdate(dec)
			if err != nil {
				closeQuietly(reader)
				return nil, err
			}
			resp.Shallow, resp.Unshallow = shallow, unshallow
		}
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			closeQuietly(reader)
			return nil, err
		}
		if typ != PktData {
			closeQuietly(reader)
			return nil, fmt.Errorf("%w: expected an ACK/NAK line, got %s", ErrProtocol, typ)
		}
		ack, err := parseAckLine(line)
		if err != nil {
			closeQuietly(reader)
			return nil, err
		}
		applyAck(neg, ack)

		if final {
			resp.Pack = wrapPack(reader, caps, req.Progress)
			return resp, nil
		}
		if ack.status == ackReady {
			forceFinal = true
		}
		closeQuietly(reader)
	}
}
