package app

import "testing"

func TestStateEnabledForNetworkCommands(t *testing.T) {
	cases := []struct {
		name  string
		state State
		id    CommandID
		want  bool
	}{
		{"clone always enabled, closed", State{}, CmdClone, true},
		{"clone always enabled, open with remotes", State{ActiveRepository: "r", HasRemotes: true}, CmdClone, true},
		{"fetch needs an open repository", State{HasRemotes: true}, CmdFetch, false},
		{"fetch needs a remote", State{ActiveRepository: "r"}, CmdFetch, false},
		{"fetch enabled with both", State{ActiveRepository: "r", HasRemotes: true}, CmdFetch, true},
		{"pull needs a remote", State{ActiveRepository: "r"}, CmdPull, false},
		{"pull enabled with both", State{ActiveRepository: "r", HasRemotes: true}, CmdPull, true},
		{"push needs a remote", State{ActiveRepository: "r"}, CmdPush, false},
		{"push enabled with both", State{ActiveRepository: "r", HasRemotes: true}, CmdPush, true},
		{"sync needs a remote", State{ActiveRepository: "r"}, CmdSync, false},
		{"sync enabled with both", State{ActiveRepository: "r", HasRemotes: true}, CmdSync, true},
		{"prune needs a remote", State{ActiveRepository: "r"}, CmdPrune, false},
		{"prune enabled with both", State{ActiveRepository: "r", HasRemotes: true}, CmdPrune, true},
		{"manage remotes needs an open repository", State{}, CmdManageRemotes, false},
		{"manage remotes does not need a remote yet", State{ActiveRepository: "r"}, CmdManageRemotes, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.Enabled(tc.id); got != tc.want {
				t.Fatalf("Enabled(%s) with state %+v = %v, want %v", tc.id, tc.state, got, tc.want)
			}
		})
	}
}
