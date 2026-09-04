package commit

import "testing"

func TestCanConfirmRequiresNonBlankMessage(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace", "   \n\t", false},
		{"text", "fix bug", true},
		{"padded text", "  fix bug  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{Message: tc.message}
			if got := m.CanConfirm(); got != tc.want {
				t.Fatalf("CanConfirm() = %v, want %v", got, tc.want)
			}
		})
	}
}
