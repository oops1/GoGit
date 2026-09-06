package credential

import (
	"context"
	"errors"
)

type Query struct {
	Protocol string
	Host     string
	Path     string
	Username string
}

type Answer struct {
	Username string
	Password []byte
}

func (a *Answer) Wipe() {
	clear(a.Password)
	a.Password = nil
	a.Username = ""
}

type Helper interface {
	Get(ctx context.Context, q Query) (Answer, bool, error)
	Store(ctx context.Context, q Query, a Answer) error
	Erase(ctx context.Context, q Query) error
	Name() string
}

type Chain []Helper

func (c Chain) Get(ctx context.Context, q Query) (Answer, bool, error) {
	var errs error
	for _, h := range c {
		ans, ok, err := h.Get(ctx, q)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if ok {
			return ans, true, nil
		}
	}
	return Answer{}, false, errs
}

func (c Chain) Store(ctx context.Context, q Query, a Answer) error {
	var errs error
	for _, h := range c {
		if err := h.Store(ctx, q, a); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func (c Chain) Erase(ctx context.Context, q Query) error {
	var errs error
	for _, h := range c {
		if err := h.Erase(ctx, q); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
