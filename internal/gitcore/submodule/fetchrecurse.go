package submodule

import "github.com/oops1/gogit/internal/gitcore/config"

type FetchRecurse int

const (
	FetchRecurseUnset FetchRecurse = iota
	FetchRecurseOn
	FetchRecurseOff
	FetchRecurseOnDemand
	FetchRecurseInvalid
)

func ParseFetchRecurse(value string, hasValue bool) FetchRecurse {
	if !hasValue {
		return FetchRecurseOn
	}
	if value == "on-demand" {
		return FetchRecurseOnDemand
	}
	on, err := config.ParseBool(value)
	switch {
	case err != nil:
		return FetchRecurseInvalid
	case on:
		return FetchRecurseOn
	}
	return FetchRecurseOff
}
