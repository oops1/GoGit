package transport

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/iana/etypeID"
	"github.com/jcmturner/gokrb5/v8/iana/nametype"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
)

const testRealm = "EXAMPLE.COM"

type kerberosFixture struct {
	keytab     *keytab.Keytab
	client     types.PrincipalName
	service    string
	tgt        messages.Ticket
	tgtKey     types.EncryptionKey
	svc        messages.Ticket
	serviceKey types.EncryptionKey
}

func newKerberosFixture(t *testing.T, serviceLifetime time.Duration) kerberosFixture {
	return newKerberosFixtureForHost(t, "git.example.com", serviceLifetime)
}

func newKerberosFixtureForHost(t *testing.T, host string, serviceLifetime time.Duration) kerberosFixture {
	t.Helper()
	testService := "HTTP/" + host
	kt := keytab.New()
	now := time.Now().UTC().Truncate(time.Second)
	for _, principal := range []string{"krbtgt/" + testRealm, testService} {
		if err := kt.AddEntry(principal, testRealm, "service-secret", now, 1, etypeID.AES256_CTS_HMAC_SHA1_96); err != nil {
			t.Fatalf("AddEntry returned %v", err)
		}
	}
	cname := types.NewPrincipalName(nametype.KRB_NT_PRINCIPAL, "alice")
	issue := func(spn string, lifetime time.Duration) (messages.Ticket, types.EncryptionKey) {
		sname := types.NewPrincipalName(nametype.KRB_NT_SRV_INST, spn)
		start := now.Add(-time.Minute)
		tkt, key, err := messages.NewTicket(cname, testRealm, sname, testRealm, types.NewKrbFlags(), kt, etypeID.AES256_CTS_HMAC_SHA1_96, 1, start, start, start.Add(lifetime), start.Add(lifetime))
		if err != nil {
			t.Fatalf("NewTicket returned %v", err)
		}
		return tkt, key
	}
	f := kerberosFixture{keytab: kt, client: cname, service: testService}
	f.tgt, f.tgtKey = issue("krbtgt/"+testRealm, time.Hour)
	f.svc, f.serviceKey = issue(testService, serviceLifetime)
	return f
}

type ccacheWriter struct {
	buf []byte
}

func (w *ccacheWriter) u16(v uint16) { w.buf = binary.BigEndian.AppendUint16(w.buf, v) }
func (w *ccacheWriter) u32(v uint32) { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }

func (w *ccacheWriter) data(b []byte) {
	w.u32(uint32(len(b)))
	w.buf = append(w.buf, b...)
}

func (w *ccacheWriter) principal(p types.PrincipalName) {
	w.u32(uint32(p.NameType))
	w.u32(uint32(len(p.NameString)))
	w.data([]byte(testRealm))
	for _, part := range p.NameString {
		w.data([]byte(part))
	}
}

func (w *ccacheWriter) credential(t *testing.T, cname types.PrincipalName, tkt messages.Ticket, key types.EncryptionKey, start, end time.Time) {
	t.Helper()
	raw, err := tkt.Marshal()
	if err != nil {
		t.Fatalf("Ticket.Marshal returned %v", err)
	}
	w.principal(cname)
	w.principal(tkt.SName)
	w.u16(uint16(key.KeyType))
	w.data(key.KeyValue)
	for _, ts := range []time.Time{start, start, end, end} {
		w.u32(uint32(ts.Unix()))
	}
	w.buf = append(w.buf, 0)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	w.data(raw)
	w.data(nil)
}

func (f kerberosFixture) writeCCache(t *testing.T, includeTGT bool, serviceEnd time.Time) string {
	t.Helper()
	w := &ccacheWriter{buf: []byte{5, 4}}
	w.u16(0)
	w.principal(f.client)
	start := time.Now().Add(-time.Minute)
	if includeTGT {
		w.credential(t, f.client, f.tgt, f.tgtKey, start, start.Add(time.Hour))
	}
	w.credential(t, f.client, f.svc, f.serviceKey, start, serviceEnd)
	path := filepath.Join(t.TempDir(), "krb5cc")
	if err := os.WriteFile(path, w.buf, 0o600); err != nil {
		t.Fatalf("WriteFile returned %v", err)
	}
	return path
}

func writeKrb5Conf(t *testing.T, kdc string) string {
	t.Helper()
	conf := "[libdefaults]\n default_realm = " + testRealm + "\n dns_lookup_kdc = false\n dns_lookup_realm = false\n udp_preference_limit = 1\n" +
		"[realms]\n " + testRealm + " = {\n  kdc = " + kdc + "\n }\n" +
		"[domain_realm]\n .example.com = " + testRealm + "\n"
	path := filepath.Join(t.TempDir(), "krb5.conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatalf("WriteFile returned %v", err)
	}
	return path
}

func useKerberosFiles(t *testing.T, confPath, ccPath string) {
	t.Helper()
	prevConf, prevCC := kerberosConfigPath, kerberosCCachePath
	kerberosConfigPath = func() string { return confPath }
	kerberosCCachePath = func() string { return ccPath }
	t.Cleanup(func() { kerberosConfigPath, kerberosCCachePath = prevConf, prevCC })
}

func refusingKDC(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return listener.Addr().String()
}

func verifyKerberosNegotiateToken(t *testing.T, f kerberosFixture, token []byte) {
	t.Helper()
	var st spnego.SPNEGOToken
	if err := st.Unmarshal(token); err != nil || !st.Init {
		t.Fatalf("token is not a SPNEGO NegTokenInit: init=%v err=%v", st.Init, err)
	}
	var krb5 spnego.KRB5Token
	if err := krb5.Unmarshal(st.NegTokenInit.MechTokenBytes); err != nil || !krb5.IsAPReq() {
		t.Fatalf("mech token is not a Kerberos AP-REQ: %v", err)
	}
	if err := krb5.APReq.Ticket.DecryptEncPart(f.keytab, nil); err != nil {
		t.Fatalf("the service cannot decrypt the ticket: %v", err)
	}
	if err := krb5.APReq.DecryptAuthenticator(f.serviceKey); err != nil {
		t.Fatalf("the authenticator is not sealed with the session key: %v", err)
	}
	if got := krb5.APReq.Authenticator.CName.PrincipalNameString(); got != "alice" {
		t.Fatalf("authenticator client = %q, want alice", got)
	}
}

func TestKerberosNegotiateTokenCarriesAnAPReqTheServiceAccepts(t *testing.T) {
	f := newKerberosFixture(t, time.Hour)
	cl := client.NewWithPassword("alice", testRealm, "unused", config.New(), client.DisablePAFXFAST(true))
	token, err := kerberosNegotiateToken(cl, f.svc, f.serviceKey)
	if err != nil {
		t.Fatalf("kerberosNegotiateToken returned %v", err)
	}
	verifyKerberosNegotiateToken(t, f, token)
}

func TestKerberosNegotiateTokenReportsBuildAndMarshalErrors(t *testing.T) {
	f := newKerberosFixture(t, time.Hour)
	cl := client.NewWithPassword("alice", testRealm, "unused", config.New())
	boom := errors.New("boom")
	prevInit, prevMarshal := newNegTokenInitKRB5, marshalSPNEGOToken
	t.Cleanup(func() { newNegTokenInitKRB5, marshalSPNEGOToken = prevInit, prevMarshal })
	newNegTokenInitKRB5 = func(*client.Client, messages.Ticket, types.EncryptionKey) (spnego.NegTokenInit, error) {
		return spnego.NegTokenInit{}, boom
	}
	if _, err := kerberosNegotiateToken(cl, f.svc, f.serviceKey); !errors.Is(err, boom) {
		t.Fatalf("build error = %v, want boom", err)
	}
	newNegTokenInitKRB5 = prevInit
	marshalSPNEGOToken = func(*spnego.SPNEGOToken) ([]byte, error) { return nil, boom }
	if _, err := kerberosNegotiateToken(cl, f.svc, f.serviceKey); !errors.Is(err, boom) {
		t.Fatalf("marshal error = %v, want boom", err)
	}
	useKerberosFiles(t, writeKrb5Conf(t, refusingKDC(t)), f.writeCCache(t, true, time.Now().Add(time.Hour)))
	if _, ok := newKerberosGenerator("git.example.com"); ok {
		t.Fatalf("newKerberosGenerator succeeded although the token could not be marshalled")
	}
}

func TestKerberosGeneratorUsesTheServiceTicketFromTheCredentialCache(t *testing.T) {
	f := newKerberosFixture(t, time.Hour)
	useKerberosFiles(t, writeKrb5Conf(t, refusingKDC(t)), f.writeCCache(t, true, time.Now().Add(time.Hour)))
	gen, ok := newKerberosGenerator("git.example.com")
	if !ok {
		t.Fatalf("newKerberosGenerator found no usable ticket")
	}
	defer gen.close()
	value, last, err := gen.next(nil)
	if err != nil || !last {
		t.Fatalf("next = last %v err %v, want a single final leg", last, err)
	}
	raw, found := strings.CutPrefix(value, "Negotiate ")
	if !found {
		t.Fatalf("authorization = %q, want a Negotiate token", value)
	}
	token, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not base64: %v", err)
	}
	verifyKerberosNegotiateToken(t, f, token)
}

func TestKerberosGeneratorFallsBackWhenKerberosIsUnusable(t *testing.T) {
	f := newKerberosFixture(t, time.Hour)
	kdc := refusingKDC(t)
	goodConf := writeKrb5Conf(t, kdc)
	goodCache := f.writeCCache(t, true, time.Now().Add(time.Hour))
	malformed := filepath.Join(t.TempDir(), "malformed")
	if err := os.WriteFile(malformed, []byte{5, 4, 0, 9}, 0o600); err != nil {
		t.Fatalf("WriteFile returned %v", err)
	}
	cases := []struct {
		name, conf, cache, host string
	}{
		{"unsupported cache type", goodConf, "", "git.example.com"},
		{"missing krb5.conf", filepath.Join(t.TempDir(), "absent.conf"), goodCache, "git.example.com"},
		{"missing cache", goodConf, filepath.Join(t.TempDir(), "absent"), "git.example.com"},
		{"malformed cache", goodConf, malformed, "git.example.com"},
		{"cache without a TGT", goodConf, f.writeCCache(t, false, time.Now().Add(time.Hour)), "git.example.com"},
		{"no ticket and no KDC", goodConf, goodCache, "other.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useKerberosFiles(t, c.conf, c.cache)
			if gen, ok := newKerberosGenerator(c.host); ok {
				gen.close()
				t.Fatalf("newKerberosGenerator succeeded, want a fallback")
			}
		})
	}
}

func TestDefaultKerberosPathsFollowTheEnvironment(t *testing.T) {
	t.Setenv("KRB5_CONFIG", "")
	if got := defaultKerberosConfigPath(); got != "/etc/krb5.conf" {
		t.Fatalf("default krb5.conf = %q", got)
	}
	t.Setenv("KRB5_CONFIG", "first.conf"+string(os.PathListSeparator)+"second.conf")
	if got := defaultKerberosConfigPath(); got != "first.conf" {
		t.Fatalf("KRB5_CONFIG path = %q, want first.conf", got)
	}
	t.Setenv("KRB5CCNAME", "")
	if got := defaultKerberosCCachePath(); !strings.HasPrefix(filepath.Base(got), "krb5cc_") {
		t.Fatalf("default cache = %q, want krb5cc_<uid>", got)
	}
	absolute := filepath.Join(t.TempDir(), "cache")
	cases := map[string]string{
		"FILE:" + absolute:        absolute,
		absolute:                  absolute,
		"KEYRING:persistent:1000": "",
		"KCM:1000":                "",
		"relative-cache":          "relative-cache",
	}
	for value, want := range cases {
		t.Setenv("KRB5CCNAME", value)
		if got := defaultKerberosCCachePath(); got != want {
			t.Errorf("KRB5CCNAME=%q gives %q, want %q", value, got, want)
		}
	}
}
