package transport

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
)

var (
	kerberosConfigPath  = defaultKerberosConfigPath
	kerberosCCachePath  = defaultKerberosCCachePath
	newNegTokenInitKRB5 = spnego.NewNegTokenInitKRB5
	marshalSPNEGOToken  = func(t *spnego.SPNEGOToken) ([]byte, error) { return t.Marshal() }

	errKerberosCCacheMalformed = errors.New("transport: the kerberos credential cache cannot be read")
)

func defaultKerberosConfigPath() string {
	if value := os.Getenv("KRB5_CONFIG"); value != "" {
		first, _, _ := strings.Cut(value, string(os.PathListSeparator))
		return first
	}
	return "/etc/krb5.conf"
}

func defaultKerberosCCachePath() string {
	name := os.Getenv("KRB5CCNAME")
	if name == "" {
		return filepath.Join(os.TempDir(), "krb5cc_"+strconv.Itoa(os.Getuid()))
	}
	if rest, ok := strings.CutPrefix(name, "FILE:"); ok {
		return rest
	}
	if strings.Contains(name, ":") && !filepath.IsAbs(name) {
		return ""
	}
	return name
}

func loadCCache(path string) (cache *credentials.CCache, err error) {
	defer func() {
		if recover() != nil {
			cache, err = nil, errKerberosCCacheMalformed
		}
	}()
	return credentials.LoadCCache(path)
}

func newKerberosClient() (*client.Client, bool) {
	ccPath := kerberosCCachePath()
	if ccPath == "" {
		return nil, false
	}
	cfg, err := config.Load(kerberosConfigPath())
	if err != nil {
		return nil, false
	}
	cache, err := loadCCache(ccPath)
	if err != nil {
		return nil, false
	}
	cl, err := client.NewFromCCache(cache, cfg, client.DisablePAFXFAST(true))
	if err != nil {
		return nil, false
	}
	return cl, true
}

func kerberosNegotiateToken(cl *client.Client, tkt messages.Ticket, key types.EncryptionKey) ([]byte, error) {
	init, err := newNegTokenInitKRB5(cl, tkt, key)
	if err != nil {
		return nil, err
	}
	return marshalSPNEGOToken(&spnego.SPNEGOToken{Init: true, NegTokenInit: init})
}

type kerberosGenerator struct {
	client *client.Client
	token  []byte
}

func newKerberosGenerator(host string) (authGenerator, bool) {
	cl, ok := newKerberosClient()
	if !ok {
		return nil, false
	}
	tkt, key, err := cl.GetServiceTicket("HTTP/" + host)
	if err == nil {
		var token []byte
		if token, err = kerberosNegotiateToken(cl, tkt, key); err == nil {
			return &kerberosGenerator{client: cl, token: token}, true
		}
	}
	cl.Destroy()
	return nil, false
}

func (g *kerberosGenerator) next([]byte) (string, bool, error) {
	return "Negotiate " + base64.StdEncoding.EncodeToString(g.token), true, nil
}

func (g *kerberosGenerator) close() {
	g.client.Destroy()
}
