package transport

import "testing"

func TestParseCapabilitiesBareAndValued(t *testing.T) {
	caps := ParseCapabilities("multi_ack side-band-64k agent=git/2.45.1 ofs-delta")
	for _, name := range []string{CapMultiAck, CapSideBand64k, CapAgent, CapOfsDelta} {
		if !caps.Has(name) {
			t.Errorf("Has(%q) = false, want true", name)
		}
	}
	if caps.Has(CapAtomic) {
		t.Errorf("Has(%q) = true, want false", CapAtomic)
	}
	if value, ok := caps.Value(CapAgent); !ok || value != "git/2.45.1" {
		t.Errorf("Value(%q) = (%q, %v), want (%q, true)", CapAgent, value, ok, "git/2.45.1")
	}
	if value, ok := caps.Value(CapMultiAck); ok || value != "" {
		t.Errorf("Value(%q) = (%q, %v), want (%q, false)", CapMultiAck, value, ok, "")
	}
}

func TestParseCapabilitiesEmptyLine(t *testing.T) {
	caps := ParseCapabilities("")
	if len(caps.Names()) != 0 {
		t.Fatalf("Names() = %v, want empty", caps.Names())
	}
	if caps.Has(CapAgent) {
		t.Fatalf("Has(%q) = true on an empty line", CapAgent)
	}
}

func TestParseCapabilitiesRepeatedKey(t *testing.T) {
	caps := ParseCapabilities("symref=HEAD:refs/heads/main symref=refs/remotes/origin/HEAD:refs/remotes/origin/main")
	values := caps.Values(CapSymref)
	if len(values) != 2 {
		t.Fatalf("Values(%q) = %v, want 2 entries", CapSymref, values)
	}
	if values[0] != "HEAD:refs/heads/main" {
		t.Errorf("Values(%q)[0] = %q", CapSymref, values[0])
	}
	names := caps.Names()
	if len(names) != 1 || names[0] != CapSymref {
		t.Fatalf("Names() = %v, want a single %q entry", names, CapSymref)
	}
}

func TestCapabilitiesAddIgnoresEmptyName(t *testing.T) {
	var caps Capabilities
	caps.Add("=orphan-value")
	if len(caps.Names()) != 0 {
		t.Fatalf("Names() = %v after adding a token with an empty name", caps.Names())
	}
}

func TestCapabilitiesAddExplicitEmptyValue(t *testing.T) {
	var caps Capabilities
	caps.Add("agent=")
	if !caps.Has(CapAgent) {
		t.Fatalf("Has(%q) = false", CapAgent)
	}
	value, ok := caps.Value(CapAgent)
	if !ok || value != "" {
		t.Fatalf("Value(%q) = (%q, %v), want (%q, true)", CapAgent, value, ok, "")
	}
}

func TestCapabilitiesValueMissingKey(t *testing.T) {
	var caps Capabilities
	if value, ok := caps.Value(CapAgent); ok || value != "" {
		t.Fatalf("Value on an empty Capabilities = (%q, %v), want (%q, false)", value, ok, "")
	}
	if caps.Values(CapAgent) != nil {
		t.Fatalf("Values on an empty Capabilities = %v, want nil", caps.Values(CapAgent))
	}
}

func TestCapabilitiesIntersect(t *testing.T) {
	ours := ParseCapabilities("multi_ack_detailed side-band-64k ofs-delta thin-pack shallow")
	remote := ParseCapabilities("multi_ack side-band-64k ofs-delta no-progress")
	got := ours.Intersect(remote)
	want := []string{CapSideBand64k, CapOfsDelta}
	names := got.Names()
	if len(names) != len(want) {
		t.Fatalf("Intersect Names() = %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("Intersect Names()[%d] = %q, want %q", i, names[i], name)
		}
	}
	if got.Has(CapMultiAckDetailed) || got.Has(CapThinPack) || got.Has(CapShallow) {
		t.Fatalf("Intersect kept a capability absent from the other side: %v", got.Names())
	}
}

func TestCapabilitiesIntersectPreservesValues(t *testing.T) {
	ours := ParseCapabilities("object-format=sha1")
	remote := ParseCapabilities("object-format=sha256")
	got := ours.Intersect(remote)
	if value, ok := got.Value(CapObjectFormat); !ok || value != "sha1" {
		t.Fatalf("Intersect Value(%q) = (%q, %v), want the receiver's own value %q", CapObjectFormat, value, ok, "sha1")
	}
}

func TestCapabilitiesIntersectEmpty(t *testing.T) {
	var ours, remote Capabilities
	got := ours.Intersect(remote)
	if len(got.Names()) != 0 {
		t.Fatalf("Intersect of two empty sets = %v, want empty", got.Names())
	}
}

func TestCapabilitiesString(t *testing.T) {
	caps := ParseCapabilities("multi_ack agent=git/2.45.1")
	if got, want := caps.String(), "multi_ack agent=git/2.45.1"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestCapabilitiesStringRoundTrips(t *testing.T) {
	line := "multi_ack_detailed side-band-64k ofs-delta thin-pack agent=git/2.45.1 symref=HEAD:refs/heads/main"
	caps := ParseCapabilities(line)
	roundTripped := ParseCapabilities(caps.String())
	if roundTripped.String() != caps.String() {
		t.Fatalf("round-tripped capabilities %q, want %q", roundTripped.String(), caps.String())
	}
}

func TestCapabilitiesStringEmpty(t *testing.T) {
	var caps Capabilities
	if got := caps.String(); got != "" {
		t.Fatalf("String() on an empty Capabilities = %q, want empty", got)
	}
}

func TestCapabilitiesNamesReturnsACopy(t *testing.T) {
	caps := ParseCapabilities("multi_ack")
	names := caps.Names()
	names[0] = "mutated"
	if caps.Names()[0] != CapMultiAck {
		t.Fatalf("mutating the slice returned by Names() affected the receiver")
	}
}
