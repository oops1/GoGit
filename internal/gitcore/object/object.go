package object

import (
	"errors"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Type uint8

const (
	TypeCommit Type = 1
	TypeTree   Type = 2
	TypeBlob   Type = 3
	TypeTag    Type = 4
)

var (
	ErrUnknownType      = errors.New("object: unknown object type")
	ErrMalformed        = errors.New("object: malformed object")
	ErrMissingHeader    = errors.New("object: required header is missing")
	ErrDuplicateHeader  = errors.New("object: duplicate header")
	ErrInvalidSignature = errors.New("object: invalid identity line")
	ErrInvalidMode      = errors.New("object: invalid tree entry mode")
	ErrInvalidHeader    = errors.New("object: invalid loose object header")
	ErrSizeMismatch     = errors.New("object: declared size differs from content size")
	ErrInvalidPath      = errors.New("object: invalid loose object path")
	ErrCorrupt          = errors.New("object: content does not match object name")
)

type Object interface {
	Type() Type
	Encode() []byte
	WriteTo(w io.Writer) (int64, error)
	ID() hash.ObjectID
}

func (t Type) String() string {
	switch t {
	case TypeCommit:
		return "commit"
	case TypeTree:
		return "tree"
	case TypeBlob:
		return "blob"
	case TypeTag:
		return "tag"
	default:
		return "unknown"
	}
}

func (t Type) Valid() bool {
	return t >= TypeCommit && t <= TypeTag
}

func ParseType(name string) (Type, error) {
	switch name {
	case "commit":
		return TypeCommit, nil
	case "tree":
		return TypeTree, nil
	case "blob":
		return TypeBlob, nil
	case "tag":
		return TypeTag, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnknownType, name)
	}
}

func Parse(objectType Type, data []byte) (Object, error) {
	var (
		parsed Object
		err    error
	)
	switch objectType {
	case TypeCommit:
		parsed, err = ParseCommit(data)
	case TypeTree:
		parsed, err = ParseTree(data)
	case TypeBlob:
		parsed, err = ParseBlob(data)
	case TypeTag:
		parsed, err = ParseTag(data)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownType, uint8(objectType))
	}
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func identify(obj Object) hash.ObjectID {
	return hash.SumSHA1(obj.Type().String(), obj.Encode())
}

func writeAll(w io.Writer, data []byte) (int64, error) {
	written, err := w.Write(data)
	return int64(written), err
}
