package transport

import (
	"context"
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func buildLsRefsRequest(agent string) []byte {
	var body []byte
	body = appendPktLine(body, "command=ls-refs\n")
	body = appendPktLine(body, "agent="+agent+"\n")
	body = appendDelimPkt(body)
	body = appendPktLine(body, "symrefs\n")
	body = appendPktLine(body, "peel\n")
	body = appendFlushPkt(body)
	return body
}

func parseLsRefsResponse(dec *Decoder) ([]Ref, string, error) {
	var refs []Ref
	head := ""
	for {
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			return nil, "", err
		}
		if typ == PktFlush {
			return refs, head, nil
		}
		if typ != PktData {
			continue
		}
		ref, symrefTarget, err := parseLsRefsLine(line)
		if err != nil {
			return nil, "", err
		}
		if ref.Name == "HEAD" && symrefTarget != "" {
			head = symrefTarget
		}
		refs = append(refs, ref)
	}
}

func parseLsRefsLine(line string) (Ref, string, error) {
	oidText, rest, ok := strings.Cut(line, " ")
	if !ok {
		return Ref{}, "", fmt.Errorf("%w: malformed ls-refs line %q", ErrProtocol, line)
	}
	id, err := hash.Parse(oidText)
	if err != nil {
		return Ref{}, "", fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	fields := strings.Split(rest, " ")
	ref := Ref{Name: fields[0], ID: id}
	symrefTarget := ""
	for _, attr := range fields[1:] {
		switch {
		case strings.HasPrefix(attr, "symref-target:"):
			symrefTarget = strings.TrimPrefix(attr, "symref-target:")
			ref.Symref = symrefTarget
		case strings.HasPrefix(attr, "peeled:"):
			peeled, err := hash.Parse(strings.TrimPrefix(attr, "peeled:"))
			if err != nil {
				return Ref{}, "", fmt.Errorf("%w: %w", ErrProtocol, err)
			}
			ref.Peeled = peeled
		}
	}
	return ref, symrefTarget, nil
}

func lsRefsV2(ctx context.Context, rt roundTripper, agent string) ([]Ref, string, error) {
	reader, err := rt.round(ctx, buildLsRefsRequest(agent))
	if err != nil {
		return nil, "", err
	}
	defer closeQuietly(reader)
	return parseLsRefsResponse(NewDecoder(reader))
}
