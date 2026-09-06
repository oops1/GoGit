package credential

import (
	"context"
	"errors"
	"testing"
)

type stubHelper struct {
	name       string
	answer     Answer
	found      bool
	getErr     error
	storeErr   error
	eraseErr   error
	storeCalls int
	eraseCalls int
}

func (s *stubHelper) Name() string { return s.name }

func (s *stubHelper) Get(context.Context, Query) (Answer, bool, error) {
	return s.answer, s.found, s.getErr
}

func (s *stubHelper) Store(context.Context, Query, Answer) error {
	s.storeCalls++
	return s.storeErr
}

func (s *stubHelper) Erase(context.Context, Query) error {
	s.eraseCalls++
	return s.eraseErr
}

func TestChainGetReturnsFirstCompleteAnswer(t *testing.T) {
	failing := &stubHelper{name: "failing", getErr: errors.New("boom")}
	empty := &stubHelper{name: "empty"}
	good := &stubHelper{name: "good", answer: Answer{Username: "u", Password: []byte("p")}, found: true}
	never := &stubHelper{name: "never", answer: Answer{Username: "x", Password: []byte("y")}, found: true}
	chain := Chain{failing, empty, good, never}
	ans, ok, err := chain.Get(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if !ok || ans.Username != "u" {
		t.Fatalf("Get = %+v, %v", ans, ok)
	}
}

func TestChainGetReturnsJoinedErrorWhenNoneAnswer(t *testing.T) {
	errA := errors.New("a failed")
	errB := errors.New("b failed")
	chain := Chain{&stubHelper{name: "a", getErr: errA}, &stubHelper{name: "b", getErr: errB}}
	_, ok, err := chain.Get(context.Background(), Query{})
	if ok {
		t.Fatalf("Get reported found")
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("Get returned %v, want both errors joined", err)
	}
}

func TestChainGetOnEmptyChainIsNotFound(t *testing.T) {
	var chain Chain
	_, ok, err := chain.Get(context.Background(), Query{})
	if ok || err != nil {
		t.Fatalf("Get on an empty chain = %v, %v", ok, err)
	}
}

func TestChainStoreCallsEveryHelperAndJoinsErrors(t *testing.T) {
	errA := errors.New("a failed")
	a := &stubHelper{name: "a", storeErr: errA}
	b := &stubHelper{name: "b"}
	chain := Chain{a, b}
	err := chain.Store(context.Background(), Query{}, Answer{})
	if !errors.Is(err, errA) {
		t.Fatalf("Store returned %v, want errA", err)
	}
	if a.storeCalls != 1 || b.storeCalls != 1 {
		t.Fatalf("Store did not call every helper: a=%d b=%d", a.storeCalls, b.storeCalls)
	}
}

func TestChainEraseCallsEveryHelperAndJoinsErrors(t *testing.T) {
	errA := errors.New("a failed")
	a := &stubHelper{name: "a", eraseErr: errA}
	b := &stubHelper{name: "b"}
	chain := Chain{a, b}
	err := chain.Erase(context.Background(), Query{})
	if !errors.Is(err, errA) {
		t.Fatalf("Erase returned %v, want errA", err)
	}
	if a.eraseCalls != 1 || b.eraseCalls != 1 {
		t.Fatalf("Erase did not call every helper: a=%d b=%d", a.eraseCalls, b.eraseCalls)
	}
}

func TestAnswerWipeZeroesPasswordAndClearsUsername(t *testing.T) {
	password := []byte("secret")
	backing := password
	a := Answer{Username: "alice", Password: password}
	a.Wipe()
	if a.Password != nil {
		t.Fatalf("Password = %v, want nil", a.Password)
	}
	if a.Username != "" {
		t.Fatalf("Username = %q, want empty", a.Username)
	}
	for i, b := range backing {
		if b != 0 {
			t.Fatalf("backing array byte %d = %d, want 0", i, b)
		}
	}
}
