package settings

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func TestSwitchChangesTravelBetweenTheConfigAndTheModel(t *testing.T) {
	for _, mode := range switchChangesOrder {
		m := FromConfig(&config.Config{Git: config.Git{SwitchChanges: mode}})
		if m.SwitchChanges != mode {
			t.Fatalf("FromConfig kept %q, want %q", m.SwitchChanges, mode)
		}
		out := &config.Config{}
		m.ApplyTo(out)
		if out.Git.SwitchChanges != mode {
			t.Fatalf("ApplyTo wrote %q, want %q", out.Git.SwitchChanges, mode)
		}
	}
	if got := (Model{SwitchChanges: "bogus"}).Normalized().SwitchChanges; got != config.SwitchChangesAsk {
		t.Fatalf("unknown mode normalized to %q", got)
	}
}

func TestTheSwitchChangesListFollowsTheModel(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{SwitchChanges: config.SwitchChangesMerge})
	if got := v.request().SwitchChanges; got != config.SwitchChangesMerge {
		t.Fatalf("request after load = %q", got)
	}
	v.switchChanges.SetSelected(3)
	if got := v.request().SwitchChanges; got != config.SwitchChangesOverwrite {
		t.Fatalf("request after picking = %q", got)
	}
}

func TestOrderLookupsFallBackToTheFirstValue(t *testing.T) {
	if orderAt(switchChangesOrder, -1) != config.SwitchChangesAsk || orderAt(switchChangesOrder, 9) != config.SwitchChangesAsk {
		t.Fatal("orderAt did not fall back to the first value")
	}
	if orderIndex(switchChangesOrder, "bogus") != 0 || orderIndex(switchChangesOrder, config.SwitchChangesStash) != 1 {
		t.Fatal("orderIndex did not find the value")
	}
}
