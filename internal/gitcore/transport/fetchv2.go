package transport

import (
	"context"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type fetchV2Round struct {
	acks       []ackLine
	shallow    []hash.ObjectID
	unshallow  []hash.ObjectID
	wantedRefs []Ref
}

func buildFetchV2Request(req FetchRequest, agent string, haves []hash.ObjectID, final bool) []byte {
	var body []byte
	body = appendPktLine(body, "command=fetch\n")
	body = appendPktLine(body, "agent="+agent+"\n")
	body = appendDelimPkt(body)
	if req.ThinPack {
		body = appendPktLine(body, "thin-pack\n")
	}
	body = appendPktLine(body, "ofs-delta\n")
	if req.Progress == nil {
		body = appendPktLine(body, "no-progress\n")
	}
	if req.IncludeTags {
		body = appendPktLine(body, "include-tag\n")
	}
	if req.Filter != "" {
		body = appendPktLine(body, "filter "+req.Filter+"\n")
	}
	for _, id := range req.Wants {
		body = appendPktLine(body, "want "+id.String()+"\n")
	}
	for _, name := range req.WantRefs {
		body = appendPktLine(body, "want-ref "+name+"\n")
	}
	for _, id := range req.Shallow {
		body = appendPktLine(body, "shallow "+id.String()+"\n")
	}
	body = appendDeepenArgs(body, req, appendPktLine)
	body = appendHaveLines(body, haves)
	if final {
		body = appendPktLine(body, "done\n")
	}
	body = appendFlushPkt(body)
	return body
}

func parseFetchV2Round(dec *Decoder) (round fetchV2Round, sawPackfile bool, err error) {
	section := ""
	for {
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			return round, false, err
		}
		if typ == PktFlush {
			return round, false, nil
		}
		if typ != PktData {
			continue
		}
		switch line {
		case "acknowledgments", "shallow-info", "wanted-refs":
			section = line
			continue
		case "packfile":
			return round, true, nil
		}
		if err := applyFetchV2SectionLine(&round, section, line); err != nil {
			return round, false, err
		}
	}
}

func applyFetchV2SectionLine(round *fetchV2Round, section, line string) error {
	switch section {
	case "acknowledgments":
		if line == "ready" {
			round.acks = append(round.acks, ackLine{status: ackReady})
			return nil
		}
		ack, err := parseAckLine(line)
		if err != nil {
			return err
		}
		round.acks = append(round.acks, ack)
		return nil
	case "shallow-info":
		return applyShallowInfoLine(round, line)
	case "wanted-refs":
		ref, err := parseWantedRefLine(line)
		if err != nil {
			return err
		}
		round.wantedRefs = append(round.wantedRefs, ref)
		return nil
	default:
		return fmt.Errorf("%w: unexpected fetch response line %q outside any section", ErrProtocol, line)
	}
}

func applyShallowInfoLine(round *fetchV2Round, line string) error {
	if rest, ok := strings.CutPrefix(line, "shallow "); ok {
		id, err := hash.Parse(rest)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrProtocol, err)
		}
		round.shallow = append(round.shallow, id)
		return nil
	}
	if rest, ok := strings.CutPrefix(line, "unshallow "); ok {
		id, err := hash.Parse(rest)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrProtocol, err)
		}
		round.unshallow = append(round.unshallow, id)
		return nil
	}
	return fmt.Errorf("%w: unexpected shallow-info line %q", ErrProtocol, line)
}

func parseWantedRefLine(line string) (Ref, error) {
	oidText, name, ok := strings.Cut(line, " ")
	if !ok {
		return Ref{}, fmt.Errorf("%w: malformed wanted-ref line %q", ErrProtocol, line)
	}
	id, err := hash.Parse(oidText)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	return Ref{Name: name, ID: id}, nil
}

func fetchV2(ctx context.Context, rt roundTripper, req FetchRequest, neg Negotiator, caps Capabilities, agent string) (*FetchResponse, error) {
	if len(req.Wants) == 0 && len(req.WantRefs) == 0 {
		return nil, fmt.Errorf("%w: fetch request has no wants", ErrProtocol)
	}
	haveNext := haveIterFunc(negHaves(ctx, neg))
	var sentHaves []hash.ObjectID
	resp := &FetchResponse{}
	forceFinal := false
	for {
		var batch []hash.ObjectID
		exhausted := forceFinal
		var err error
		if !forceFinal {
			batch, exhausted, err = pullHaves(haveNext, maxHavesPerRound)
			if err != nil {
				return nil, err
			}
		}
		sentHaves = append(sentHaves, batch...)
		final := forceFinal || exhausted || (neg != nil && neg.Enough())

		body := buildFetchV2Request(req, agent, sentHaves, final)
		reader, err := rt.round(ctx, body)
		if err != nil {
			return nil, err
		}
		dec := NewDecoder(reader)
		round, sawPackfile, err := parseFetchV2Round(dec)
		if err != nil {
			closeQuietly(reader)
			return nil, err
		}
		for _, ack := range round.acks {
			applyAck(neg, ack)
		}
		resp.Shallow = append(resp.Shallow, round.shallow...)
		resp.Unshallow = append(resp.Unshallow, round.unshallow...)
		resp.WantedRefs = append(resp.WantedRefs, round.wantedRefs...)

		if sawPackfile {
			resp.Pack = readCloser{Reader: NewSidebandReader(reader, req.Progress), closer: reader.Close}
			return resp, nil
		}
		if final {
			closeQuietly(reader)
			return nil, fmt.Errorf("%w: server did not send a packfile after done", ErrProtocol)
		}
		if hasReadyAck(round.acks) {
			forceFinal = true
		}
		closeQuietly(reader)
	}
}

func hasReadyAck(acks []ackLine) bool {
	for _, ack := range acks {
		if ack.status == ackReady {
			return true
		}
	}
	return false
}
