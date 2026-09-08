package reposettings

import "testing"

func TestANameIsRequired(t *testing.T) {
	if hint := Validate(Settings{Name: "   "}); hint.Key != hintNameRequired || hint.OK {
		t.Fatalf("hint = %+v, want the name request", hint)
	}
}

func TestAnAddressThatIsNotAnEmailIsRefused(t *testing.T) {
	for _, address := range []string{"ann", "@example.com", "ann@", "ann@example", "a b@example.com"} {
		hint := Validate(Settings{Name: "Main", UserName: "Ann", UserEmail: address})
		if hint.Key != hintEmailInvalid || hint.OK {
			t.Fatalf("%q: hint = %+v, want the email refusal", address, hint)
		}
	}
}

func TestHalfAnIdentityIsRefused(t *testing.T) {
	for _, s := range []Settings{
		{Name: "Main", UserName: "Ann"},
		{Name: "Main", UserEmail: "ann@example.com"},
	} {
		if hint := Validate(s); hint.Key != hintIdentityHalf || hint.OK {
			t.Fatalf("%+v: hint = %+v, want both halves demanded", s, hint)
		}
	}
}

func TestAWholeIdentityAndAnEmptyOneAreAccepted(t *testing.T) {
	for _, s := range []Settings{
		{Name: "Main"},
		{Name: "Main", UserName: "Ann", UserEmail: "ann@example.com"},
	} {
		if hint := Validate(s); !hint.OK || hint.Key != hintReadyToSave {
			t.Fatalf("%+v: hint = %+v, want it accepted", s, hint)
		}
	}
}

func TestSpacesAroundTheValuesAreDropped(t *testing.T) {
	got := Normalise(Settings{
		Name:          "  Main  ",
		UserName:      " Ann ",
		UserEmail:     " ann@example.com ",
		DefaultRemote: " origin ",
	})

	want := Settings{Name: "Main", UserName: "Ann", UserEmail: "ann@example.com", DefaultRemote: "origin"}
	if got != want {
		t.Fatalf("normalised = %+v, want %+v", got, want)
	}
}
