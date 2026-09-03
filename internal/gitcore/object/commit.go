package object

import (
	"bytes"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	headerTree      = "tree"
	headerParent    = "parent"
	headerAuthor    = "author"
	headerCommitter = "committer"
	headerEncoding  = "encoding"
	headerGPGSig    = "gpgsig"
	headerObject    = "object"
	headerType      = "type"
	headerTag       = "tag"
	headerTagger    = "tagger"
)

type Commit struct {
	Tree         hash.ObjectID
	Parents      []hash.ObjectID
	Author       Signature
	Committer    Signature
	Encoding     string
	GPGSignature string
	Extra        []ExtraHeader
	Message      string
}

func ParseCommit(data []byte) (*Commit, error) {
	headers, message, err := splitHeaders(data)
	if err != nil {
		return nil, err
	}
	commit := new(Commit{Message: message})
	seen := make(headerSet)
	for _, header := range headers {
		switch header.Key {
		case headerTree, headerAuthor, headerCommitter, headerEncoding, headerGPGSig:
			if err := seen.add(header.Key); err != nil {
				return nil, err
			}
		}
		switch header.Key {
		case headerTree:
			commit.Tree, err = parseHeaderID(header)
		case headerParent:
			var parent hash.ObjectID
			if parent, err = parseHeaderID(header); err == nil {
				commit.Parents = append(commit.Parents, parent)
			}
		case headerAuthor:
			commit.Author, err = parseHeaderSignature(header)
		case headerCommitter:
			commit.Committer, err = parseHeaderSignature(header)
		case headerEncoding:
			commit.Encoding = header.Value
		case headerGPGSig:
			commit.GPGSignature = header.Value
		default:
			commit.Extra = append(commit.Extra, header)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := seen.require(headerTree, headerAuthor, headerCommitter); err != nil {
		return nil, err
	}
	return commit, nil
}

func (c *Commit) Type() Type {
	return TypeCommit
}

func (c *Commit) Encode() []byte {
	var buf bytes.Buffer
	writeHeader(&buf, headerTree, c.Tree.String())
	for _, parent := range c.Parents {
		writeHeader(&buf, headerParent, parent.String())
	}
	writeHeader(&buf, headerAuthor, c.Author.String())
	writeHeader(&buf, headerCommitter, c.Committer.String())
	if c.Encoding != "" {
		writeHeader(&buf, headerEncoding, c.Encoding)
	}
	for _, extra := range c.Extra {
		writeHeader(&buf, extra.Key, extra.Value)
	}
	if c.GPGSignature != "" {
		writeHeader(&buf, headerGPGSig, c.GPGSignature)
	}
	buf.WriteByte('\n')
	buf.WriteString(c.Message)
	return buf.Bytes()
}

func (c *Commit) WriteTo(w io.Writer) (int64, error) {
	return writeAll(w, c.Encode())
}

func (c *Commit) ID() hash.ObjectID {
	return identify(c)
}

func parseHeaderID(header ExtraHeader) (hash.ObjectID, error) {
	id, err := hash.Parse(header.Value)
	if err != nil {
		return hash.Zero, fmt.Errorf("%w: header %s: %w", ErrMalformed, header.Key, err)
	}
	return id, nil
}

func parseHeaderSignature(header ExtraHeader) (Signature, error) {
	signature, err := ParseSignature([]byte(header.Value))
	if err != nil {
		return Signature{}, fmt.Errorf("header %s: %w", header.Key, err)
	}
	return signature, nil
}
