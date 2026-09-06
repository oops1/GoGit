package transport

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

type Service string

const (
	UploadPack  Service = "git-upload-pack"
	ReceivePack Service = "git-receive-pack"
)

type Credentials struct {
	Username string
	Password []byte
	Token    []byte
}

func (c *Credentials) Wipe() {
	for i := range c.Password {
		c.Password[i] = 0
	}
	c.Password = nil
	for i := range c.Token {
		c.Token[i] = 0
	}
	c.Token = nil
}

type CredentialSource interface {
	Credentials(ctx context.Context, resource string, retry bool) (Credentials, error)
}

type Ref struct {
	Name   string
	ID     hash.ObjectID
	Peeled hash.ObjectID
	Symref string
}

type Advertisement struct {
	Version      int
	Refs         []Ref
	Capabilities Capabilities
	Shallow      []hash.ObjectID
	Head         string
}

type FetchRequest struct {
	Wants       []hash.ObjectID
	WantRefs    []string
	Shallow     []hash.ObjectID
	Depth       int
	DeepenSince time.Time
	DeepenNot   []string
	Filter      string
	IncludeTags bool
	ThinPack    bool
	Progress    progress.Func
}

type FetchResponse struct {
	Common     []hash.ObjectID
	Shallow    []hash.ObjectID
	Unshallow  []hash.ObjectID
	WantedRefs []Ref
	Pack       io.ReadCloser
}

type Negotiator interface {
	Haves(ctx context.Context) iter.Seq2[hash.ObjectID, error]
	Common(id hash.ObjectID)
	Enough() bool
}

type Update struct {
	Name string
	Old  hash.ObjectID
	New  hash.ObjectID
}

type RefStatus struct {
	Name    string
	OK      bool
	Message string
}

type PushRequest struct {
	Updates  []Update
	Pack     io.Reader
	Atomic   bool
	Options  []string
	Progress progress.Func
}

type PushResult struct {
	UnpackOK    bool
	UnpackError string
	Refs        []RefStatus
}

type Session interface {
	Advertise(ctx context.Context) (Advertisement, error)
	Fetch(ctx context.Context, req FetchRequest, neg Negotiator) (*FetchResponse, error)
	Push(ctx context.Context, req PushRequest) (*PushResult, error)
	Close() error
}

type Options struct {
	Credentials CredentialSource
	UserAgent   string
	Version     int
	HTTPClient  *http.Client
	Progress    progress.Func
	HostKeys    HostKeyPolicy
	Keys        KeySource
}

type HostKey struct {
	Host        string
	Algorithm   string
	Fingerprint string
	Key         []byte
}

type HostKeyPolicy interface {
	Check(ctx context.Context, key HostKey) error
	Accept(ctx context.Context, key HostKey) error
}

type KeySource interface {
	Keys(ctx context.Context, host string) ([]Key, error)
}

type Key struct {
	Path       string
	Private    []byte
	Passphrase []byte
}

func Dial(ctx context.Context, rawURL string, service Service, opts Options) (Session, error) {
	endpoint, password, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	switch endpoint.Scheme {
	case SchemeHTTP, SchemeHTTPS:
		return newHTTPSession(endpoint, password, service, opts), nil
	case SchemeGit:
		password.Wipe()
		return newGitSession(endpoint, service, opts), nil
	case SchemeSSH:
		password.Wipe()
		return newSSHSession(endpoint, service, opts), nil
	default:
		password.Wipe()
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedScheme, endpoint.Scheme)
	}
}
