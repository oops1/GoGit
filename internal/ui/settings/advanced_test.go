package settings

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
)

func TestGitAdvancedGroupStartsCollapsed(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	if v.gitAdvanced.IsExpanded {
		t.Fatal("advanced git settings must start collapsed")
	}
}

func TestExpandingAdvancedGitSettingsGrowsTheExpanderAndPreservesValues(t *testing.T) {
	initial := Model{
		WorkTreeDepth: 7,
		PullStrategy:  config.PullStrategyRebase,
		ShallowDepth:  20,
	}
	v := newTestView(t, []string{"en"}, initial)
	v.SetSection("git")

	collapsedHeight := v.gitAdvanced.Bounds().Dy()

	v.gitAdvanced.SetExpanded(true)

	if !v.gitAdvanced.IsExpanded {
		t.Fatal("expander must report expanded after SetExpanded(true)")
	}
	expandedHeight := v.gitAdvanced.Bounds().Dy()
	if expandedHeight <= collapsedHeight {
		t.Fatalf("expanded height = %d, want more than collapsed height %d", expandedHeight, collapsedHeight)
	}

	if v.workTreeDepth.Value() != 7 {
		t.Fatalf("workTreeDepth = %v, want 7", v.workTreeDepth.Value())
	}
	if v.pullStrategy.GetText() != config.PullStrategyRebase {
		t.Fatalf("pullStrategy = %q, want %q", v.pullStrategy.GetText(), config.PullStrategyRebase)
	}
	if v.shallowDepth.Value() != 20 {
		t.Fatalf("shallowDepth = %v, want 20", v.shallowDepth.Value())
	}

	v.gitAdvanced.SetExpanded(false)

	if v.gitAdvanced.Bounds().Dy() != collapsedHeight {
		t.Fatalf("collapsed height after re-collapsing = %d, want %d", v.gitAdvanced.Bounds().Dy(), collapsedHeight)
	}
	if v.workTreeDepth.Value() != 7 {
		t.Fatal("collapsing must not reset workTreeDepth")
	}
	if v.pullStrategy.GetText() != config.PullStrategyRebase {
		t.Fatal("collapsing must not reset pullStrategy")
	}
	if v.shallowDepth.Value() != 20 {
		t.Fatal("collapsing must not reset shallowDepth")
	}
}

func TestExpandingAdvancedGitSettingsAllowsEditingHiddenFields(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")

	v.gitAdvanced.SetExpanded(true)
	v.workTreeDepth.SetValue(9)
	v.pullStrategy.SetText(config.PullStrategyMerge)
	v.shallowDepth.SetValue(42)

	got := v.request()
	if got.WorkTreeDepth != 9 {
		t.Fatalf("WorkTreeDepth = %d, want 9", got.WorkTreeDepth)
	}
	if got.PullStrategy != config.PullStrategyMerge {
		t.Fatalf("PullStrategy = %q, want %q", got.PullStrategy, config.PullStrategyMerge)
	}
	if got.ShallowDepth != 42 {
		t.Fatalf("ShallowDepth = %d, want 42", got.ShallowDepth)
	}
}
