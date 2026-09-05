package transport

import (
	"context"
	"errors"
	"io"
	"iter"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func startFakeGitServer(t *testing.T, handle func(t *testing.T, conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		handle(t, conn)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("fake git server did not finish within 5s")
		}
	})
	return ln.Addr().String()
}

func gitV1AdvertisementBytes(headCaps string, refs [][2]string) []byte {
	b := newPktBuilder()
	for i, r := range refs {
		if i == 0 {
			b.line(r[0] + " " + r[1] + "\x00" + headCaps + "\n")
			continue
		}
		b.line(r[0] + " " + r[1] + "\n")
	}
	b.flush()
	return b.bytes()
}

func gitV2AdvertisementBytes(capLines []string) []byte {
	b := newPktBuilder()
	b.line("version 2\n")
	for _, c := range capLines {
		b.line(c + "\n")
	}
	b.flush()
	return b.bytes()
}

func readHaveRound(t *testing.T, dec *Decoder) (haveCount int, isDone bool) {
	t.Helper()
	for {
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			t.Errorf("readOnePktLine returned error %v", err)
			return haveCount, false
		}
		if typ == PktFlush {
			return haveCount, false
		}
		if typ != PktData {
			continue
		}
		if line == "done" {
			return haveCount, true
		}
		if strings.HasPrefix(line, "have ") {
			haveCount++
		}
	}
}

func TestGitAdvertiseV1(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("server did not receive a request line: %v", dec.Err())
			return
		}
		line := string(dec.Bytes())
		if !strings.HasPrefix(line, "git-upload-pack /repo.git\x00host=") {
			t.Errorf("request line = %q", line)
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{
			{idOf(1).String(), "HEAD"},
			{idOf(1).String(), "refs/heads/main"},
			{idOf(2).String(), "refs/heads/other"},
		})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 1 {
		t.Fatalf("Version = %d, want 1", adv.Version)
	}
	if len(adv.Refs) != 3 {
		t.Fatalf("Refs = %v, want 3 entries", adv.Refs)
	}
}

func TestGitAdvertiseV2ListsRefsThroughLsRefs(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("server did not receive a request line: %v", dec.Err())
			return
		}
		line := string(dec.Bytes())
		if !strings.Contains(line, "\x00version=2\x00") {
			t.Errorf("request line = %q, want a version=2 extra parameter", line)
			return
		}
		if _, err := conn.Write(gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow", "agent=git/test"})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		line2, typ, err := readOnePktLine(dec)
		if err != nil || typ != PktData || line2 != "command=ls-refs" {
			t.Errorf("expected a ls-refs command, got %q type %v err %v", line2, typ, err)
			return
		}
		for {
			_, typ, err := readOnePktLine(dec)
			if err != nil {
				t.Errorf("readOnePktLine returned error %v", err)
				return
			}
			if typ == PktFlush {
				break
			}
		}
		resp := newPktBuilder().
			line(idOf(1).String() + " HEAD symref-target:refs/heads/main\n").
			line(idOf(1).String() + " refs/heads/main\n").
			flush().bytes()
		if _, err := conn.Write(resp); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 2 {
		t.Fatalf("Version = %d, want 2", adv.Version)
	}
	if adv.Head != "refs/heads/main" {
		t.Fatalf("Head = %q, want refs/heads/main", adv.Head)
	}
	if len(adv.Refs) != 2 {
		t.Fatalf("Refs = %v, want 2 entries", adv.Refs)
	}
}

func TestGitFetchV1WithSidebandPack(t *testing.T) {
	pack := fakePack(11)
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		n, done := readHaveRound(t, dec)
		if n != 0 || !done {
			t.Errorf("round = (haves=%d, done=%v), want (0, true)", n, done)
			return
		}
		resp := newPktBuilder().line("NAK\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes()
		if _, err := conn.Write(resp); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

func TestGitFetchV2SendsPackfileAfterDone(t *testing.T) {
	pack := fakePack(13)
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow"})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		if _, typ, err := readOnePktLine(dec); err != nil || typ != PktData {
			t.Errorf("expected ls-refs command, err=%v", err)
			return
		}
		for {
			_, typ, err := readOnePktLine(dec)
			if err != nil {
				t.Errorf("readOnePktLine: %v", err)
				return
			}
			if typ == PktFlush {
				break
			}
		}
		if _, err := conn.Write(newPktBuilder().line(idOf(1).String() + " refs/heads/main\n").flush().bytes()); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		if _, typ, err := readOnePktLine(dec); err != nil || typ != PktData {
			t.Errorf("expected fetch command, err=%v", err)
			return
		}
		var sawDone bool
		for {
			line, typ, err := readOnePktLine(dec)
			if err != nil {
				t.Errorf("readOnePktLine: %v", err)
				return
			}
			if typ == PktFlush {
				break
			}
			if line == "done" {
				sawDone = true
			}
		}
		if !sawDone {
			t.Errorf("fetch command never sent done")
			return
		}
		if _, err := conn.Write(fetchV2PackfileBody(pack)); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
}

type sliceNegotiator struct {
	haves  []hash.ObjectID
	common []hash.ObjectID
}

func (n *sliceNegotiator) Haves(context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		for _, id := range n.haves {
			if !yield(id, nil) {
				return
			}
		}
	}
}

func (n *sliceNegotiator) Common(id hash.ObjectID) {
	n.common = append(n.common, id)
}

func (n *sliceNegotiator) Enough() bool {
	return false
}

func TestGitFetchV1NegotiationStopsAtServerReady(t *testing.T) {
	const total = 100
	haves := make([]hash.ObjectID, total)
	for i := range haves {
		haves[i] = idOf(byte(20 + i))
	}
	pack := fakePack(17)
	var totalHaves, round3Haves int
	var round1Final, round2Final, round3Final bool

	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		n1, done1 := readHaveRound(t, dec)
		totalHaves += n1
		round1Final = done1
		if _, err := conn.Write(newPktBuilder().line("NAK\n").bytes()); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		n2, done2 := readHaveRound(t, dec)
		totalHaves += n2
		round2Final = done2
		readyID := haves[total-1]
		if _, err := conn.Write(newPktBuilder().line("ACK " + readyID.String() + " ready\n").bytes()); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		n3, done3 := readHaveRound(t, dec)
		round3Haves = n3
		round3Final = done3
		resp := newPktBuilder().line("ACK " + haves[0].String() + "\n").raw(sidebandFrame(SidebandPack, pack)).flush().bytes()
		if _, err := conn.Write(resp); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	neg := &sliceNegotiator{haves: haves}
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, neg)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()
	got, err := io.ReadAll(resp.Pack)
	if err != nil {
		t.Fatalf("reading Pack returned error %v", err)
	}
	if string(got) != string(pack) {
		t.Fatalf("Pack = %x, want %x", got, pack)
	}
	if round1Final || round2Final {
		t.Fatalf("round1Final=%v round2Final=%v, want both false", round1Final, round2Final)
	}
	if !round3Final {
		t.Fatalf("round3Final = false, want true (client must send done after ready)")
	}
	if round3Haves != 0 {
		t.Fatalf("round3 sent %d have lines after ready, want 0", round3Haves)
	}
	if totalHaves != 64 {
		t.Fatalf("client sent %d have lines total, want 64 (two batches of 32, then stop)", totalHaves)
	}
	if len(neg.common) == 0 {
		t.Fatalf("Negotiator.Common was never called")
	}
}

func TestGitPushReportStatus(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		line, typ, err := readOnePktLine(dec)
		if err != nil || typ != PktData || !strings.HasPrefix(line, idOf(1).String()+" "+idOf(2).String()+" refs/heads/main") {
			t.Errorf("update command = %q, err=%v", line, err)
			return
		}
		for {
			_, typ, err := readOnePktLine(dec)
			if err != nil {
				t.Errorf("readOnePktLine: %v", err)
				return
			}
			if typ == PktFlush {
				break
			}
		}
		packBuf := make([]byte, len("PACKDATA"))
		if _, err := io.ReadFull(conn, packBuf); err != nil {
			t.Errorf("reading pack bytes returned error %v", err)
			return
		}
		if string(packBuf) != "PACKDATA" {
			t.Errorf("pack bytes = %q, want PACKDATA", packBuf)
		}
		if _, err := conn.Write(reportStatusBody("ok", []string{"ok refs/heads/main"})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    strings.NewReader("PACKDATA"),
	})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !result.UnpackOK {
		t.Fatalf("UnpackOK = false, want true")
	}
	if len(result.Refs) != 1 || !result.Refs[0].OK {
		t.Fatalf("Refs = %+v", result.Refs)
	}
}

func TestGitFetchCancelledByContext(t *testing.T) {
	release := make(chan struct{})
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		<-release
	})
	defer close(release)

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded despite a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want context.Canceled", err)
	}
}

func TestGitRequestLineFormat(t *testing.T) {
	tests := []struct {
		name    string
		version int
		want    string
	}{
		{"v1", 1, "git-upload-pack /repo.git\x00host=127.0.0.1\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
				dec := NewDecoder(conn)
				if !dec.Scan() {
					t.Errorf("no request line: %v", dec.Err())
					return
				}
				got = string(dec.Bytes())
			})
			session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: tt.version})
			if err != nil {
				t.Fatalf("Dial returned error %v", err)
			}
			defer func() { _ = session.Close() }()
			_, _ = session.Advertise(t.Context())
			if !strings.HasPrefix(got, "git-upload-pack /repo.git\x00host=") {
				t.Fatalf("request line = %q", got)
			}
			if tt.version == 1 && strings.Contains(got, "version=2") {
				t.Fatalf("request line = %q, must not request version=2", got)
			}
		})
	}
}

func TestGitRequestLineIncludesVersionTwoByDefault(t *testing.T) {
	var got string
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		got = string(dec.Bytes())
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, _ = session.Advertise(t.Context())
	want := "git-upload-pack /repo.git\x00host=127.0.0.1\x00\x00version=2\x00"
	if got != want {
		t.Fatalf("request line = %q, want %q", got, want)
	}
}

func TestGitSessionClosedRejectsEveryMethod(t *testing.T) {
	session, err := Dial(t.Context(), "git://127.0.0.1:1/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close returned error %v", err)
	}
	if _, err := session.Advertise(t.Context()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Advertise on a closed session returned %v, want ErrProtocol", err)
	}
	if _, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Fetch on a closed session returned %v, want ErrProtocol", err)
	}
	if _, err := session.Push(t.Context(), PushRequest{Pack: strings.NewReader("")}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Push on a closed session returned %v, want ErrProtocol", err)
	}
}

func TestGitCloseWithoutConnecting(t *testing.T) {
	session, err := Dial(t.Context(), "git://127.0.0.1:1/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close on an unconnected session returned error %v", err)
	}
}

func TestGitConnectDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded against a closed listener")
	}
}

func TestGitAdvertiseFailsWhenServerClosesBeforeResponding(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded despite the server closing the connection")
	}
}

func TestGitPushFailsWhenAdvertiseFails(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	_, err = session.Push(t.Context(), PushRequest{Pack: strings.NewReader("")})
	if err == nil {
		t.Fatalf("Push succeeded despite the server closing the connection")
	}
}

func TestGitEnsureAdvertisedSkipsASecondRequest(t *testing.T) {
	var requestLines atomic.Int32
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		requestLines.Add(1)
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
			return
		}
		n, done := readHaveRound(t, dec)
		if n != 0 || !done {
			t.Errorf("round = (haves=%d, done=%v)", n, done)
			return
		}
		if _, err := conn.Write(fetchV1ResponseBody("NAK", fakePack(53))); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	_ = resp.Pack.Close()
	if got := requestLines.Load(); got != 1 {
		t.Fatalf("server saw %d request lines, want 1 (no re-dial/re-advertise)", got)
	}
}

func TestGitFetchWriteErrorAfterConnectionCloses(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	gs := session.(*gitSession)
	if err := gs.conn.Close(); err != nil {
		t.Fatalf("closing the underlying connection returned error %v", err)
	}
	_, err = session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err == nil {
		t.Fatalf("Fetch succeeded despite a closed connection")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned context.Canceled, want a plain write error")
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestGitConnectUsesDefaultPortWhenEndpointOmitsOne(t *testing.T) {
	session, err := Dial(t.Context(), "git://127.0.0.1/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err = session.Advertise(ctx)
	if err == nil {
		t.Fatalf("Advertise succeeded against 127.0.0.1 with no git daemon listening")
	}
	if !strings.Contains(err.Error(), defaultGitPort) {
		t.Fatalf("Advertise error %q does not mention the default port %q", err.Error(), defaultGitPort)
	}
}

func TestGitRoundReturnsContextErrorWhenWriteFailsAfterCancellation(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}

	gs := session.(*gitSession)
	if err := gs.conn.SetDeadline(time.Now()); err != nil {
		t.Fatalf("SetDeadline returned error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want context.Canceled", err)
	}
}

func TestGitAdvertiseV2FailsWhenLsRefsFails(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV2AdvertisementBytes([]string{"ls-refs", "fetch=shallow"})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Advertise(t.Context()); err == nil {
		t.Fatalf("Advertise succeeded despite the server closing the connection before answering ls-refs")
	}
}

func TestGitAdvertiseReturnsContextErrorWhenReadFailsAfterCancellation(t *testing.T) {
	release := make(chan struct{})
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
		<-release
	})
	defer close(release)

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	gs := session.(*gitSession)
	if err := gs.connect(t.Context()); err != nil {
		t.Fatalf("connect returned error %v", err)
	}
	if err := gs.conn.SetDeadline(time.Now()); err != nil {
		t.Fatalf("SetDeadline returned error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = session.Advertise(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise returned %v, want context.Canceled", err)
	}
}

func TestGitFetchFailsWhenConnectFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil); err == nil {
		t.Fatalf("Fetch succeeded against a closed listener")
	}
}

func TestGitFetchFailsWhenAdvertiseFails(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil); err == nil {
		t.Fatalf("Fetch succeeded despite the server closing the connection before advertising")
	}
}

func TestGitFetchReturnsContextErrorWhenAdvertiseFailsAfterCancellation(t *testing.T) {
	release := make(chan struct{})
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
		<-release
	})
	defer close(release)

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	gs := session.(*gitSession)
	if err := gs.connect(t.Context()); err != nil {
		t.Fatalf("connect returned error %v", err)
	}
	if err := gs.conn.SetDeadline(time.Now()); err != nil {
		t.Fatalf("SetDeadline returned error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = session.Fetch(ctx, FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want context.Canceled", err)
	}
}

func TestGitPushFailsWhenConnectFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Push(t.Context(), PushRequest{Pack: strings.NewReader("")}); err == nil {
		t.Fatalf("Push succeeded against a closed listener")
	}
}

func TestGitPushReturnsContextErrorWhenAdvertiseFailsAfterCancellation(t *testing.T) {
	release := make(chan struct{})
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		dec.Scan()
		<-release
	})
	defer close(release)

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	gs := session.(*gitSession)
	if err := gs.connect(t.Context()); err != nil {
		t.Fatalf("connect returned error %v", err)
	}
	if err := gs.conn.SetDeadline(time.Now()); err != nil {
		t.Fatalf("SetDeadline returned error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = session.Push(ctx, PushRequest{Pack: strings.NewReader("")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Push returned %v, want context.Canceled", err)
	}
}

func TestGitPushFailsWhenPrefixWriteFails(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}

	gs := session.(*gitSession)
	if err := gs.conn.Close(); err != nil {
		t.Fatalf("closing the underlying connection returned error %v", err)
	}
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    strings.NewReader("PACKDATA"),
	})
	if err == nil {
		t.Fatalf("Push succeeded despite a closed connection")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Push returned context.Canceled, want a plain write error")
	}
}

func TestGitPushFailsWhenPackReadFails(t *testing.T) {
	addr := startFakeGitServer(t, func(t *testing.T, conn net.Conn) {
		dec := NewDecoder(conn)
		if !dec.Scan() {
			t.Errorf("no request line: %v", dec.Err())
			return
		}
		if _, err := conn.Write(gitV1AdvertisementBytes(testPushHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}})); err != nil {
			t.Errorf("Write returned error %v", err)
		}
	})
	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", ReceivePack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	boom := errors.New("boom")
	_, err = session.Push(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: idOf(1), New: idOf(2)}},
		Pack:    errReader{err: boom},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Push returned %v, want boom", err)
	}
}
