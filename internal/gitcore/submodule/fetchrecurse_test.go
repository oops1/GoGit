package submodule

import "testing"

func TestParseFetchRecurseFollowsGit(t *testing.T) {
	tests := []struct {
		value    string
		hasValue bool
		want     FetchRecurse
	}{
		{"", false, FetchRecurseOn},
		{"true", true, FetchRecurseOn},
		{"yes", true, FetchRecurseOn},
		{"off", true, FetchRecurseOff},
		{"on-demand", true, FetchRecurseOnDemand},
		{"On-Demand", true, FetchRecurseInvalid},
		{"sometimes", true, FetchRecurseInvalid},
	}
	for _, tt := range tests {
		if got := ParseFetchRecurse(tt.value, tt.hasValue); got != tt.want {
			t.Fatalf("ParseFetchRecurse(%q, %v) = %v, want %v", tt.value, tt.hasValue, got, tt.want)
		}
	}
}
