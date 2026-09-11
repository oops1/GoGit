package trailer

import "testing"

func TestTheTrailersOfAMessageAreTheLastParagraphOfKeyedLines(t *testing.T) {
	message := "subject\n\nbody line\n\nSigned-off-by: Ann <ann@example.com>\nCo-authored-by: Bob <bob@example.com>\n"

	body, lines, ok := Block(message)

	if !ok || body != "subject\n\nbody line" {
		t.Fatalf("body = %q, ok = %v", body, ok)
	}
	if len(lines) != 2 || lines[0].Key != "Signed-off-by" || lines[1].Value != "Bob <bob@example.com>" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestAParagraphThatIsNotAllKeyedLinesIsNoTrailerBlock(t *testing.T) {
	for _, message := range []string{
		"subject\n",
		"subject\n\njust prose\n",
		"subject\n\nCo-authored-by: Ann <ann@example.com>\nand a loose line\n",
		"subject\n\n: no key\n",
		"subject\n\n-leading-dash: value\n",
	} {
		if _, _, ok := Block(message); ok {
			t.Fatalf("%q must not read as a trailer block", message)
		}
	}
}

func TestAWrappedTrailerValueStaysWithItsKey(t *testing.T) {
	message := "subject\n\nHelped-by: Ann\n  of the second team\n"

	_, lines, ok := Block(message)

	if !ok || len(lines) != 1 || lines[0].Value != "Ann\n  of the second team" {
		t.Fatalf("lines = %+v, ok = %v", lines, ok)
	}
}

func TestEveryTrailerThatNamesSomebodyElseIsAnAttribution(t *testing.T) {
	for _, key := range []string{
		"Co-authored-by", "co-authored-by", "Signed-off-by", "Helped-by",
		"Suggested-by", "Reported-by", "Reviewed-by", "Tested-by", "Acked-by",
		"Thanks-to", "Attributed-to",
	} {
		if !IsAttribution(key) {
			t.Fatalf("%q must count as an attribution", key)
		}
	}
	for _, key := range []string{"Fixes", "Refs", "Change-Id", "Bug", "Link"} {
		if IsAttribution(key) {
			t.Fatalf("%q must be left alone", key)
		}
	}
}

func TestAttributionTrailersAreTakenOutAndTheRestStays(t *testing.T) {
	message := "subject\n\nbody\n\nFixes: #12\nCo-authored-by: Bob <bob@example.com>\nSigned-off-by: Ann <ann@example.com>\n"

	got := WithoutAttribution(message)

	want := "subject\n\nbody\n\nFixes: #12\n"
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestAMessageThatIsNothingButAttributionKeepsOnlyItsText(t *testing.T) {
	message := "subject\n\nbody\n\nCo-authored-by: Bob <bob@example.com>\n"

	got := WithoutAttribution(message)

	if got != "subject\n\nbody\n" {
		t.Fatalf("message = %q, want the text without the trailer paragraph", got)
	}
}

func TestAMessageWithoutAttributionComesBackUntouched(t *testing.T) {
	for _, message := range []string{
		"subject\n",
		"subject\n\nbody\n",
		"subject\n\nFixes: #12\n",
		"subject\n\nnot a trailer: but prose with a colon\n",
	} {
		if got := WithoutAttribution(message); got != message {
			t.Fatalf("message = %q, want it unchanged (%q)", got, message)
		}
	}
}

func TestThePeopleAMessageCreditsAreReadInOrderOnce(t *testing.T) {
	message := "subject\n\nFixes: #12\nCo-Authored-By: Claude Opus 5 <noreply@anthropic.com>\n" +
		"Helped-by: Bob <bob@example.com>\nReviewed-by: Claude Opus 5 <noreply@anthropic.com>\n"

	got := Attributed(message)

	if len(got) != 2 || got[0] != "Claude Opus 5" || got[1] != "Bob" {
		t.Fatalf("people = %q, want each credited person once, in the order of the message", got)
	}
}

func TestAPersonWithoutANameIsKnownByTheAddress(t *testing.T) {
	message := "subject\n\nCo-authored-by: <bob@example.com>\nSuggested-by: Ann\nThanks-to:  \n"

	got := Attributed(message)

	if len(got) != 2 || got[0] != "bob@example.com" || got[1] != "Ann" {
		t.Fatalf("people = %q, want the address when there is no name and the bare name as written", got)
	}
}

func TestAMessageThatCreditsNobodyNamesNobody(t *testing.T) {
	for _, message := range []string{"subject\n", "subject\n\nbody\n", "subject\n\nFixes: #12\n"} {
		if got := Attributed(message); len(got) != 0 {
			t.Fatalf("people = %q for %q, want none", got, message)
		}
	}
}
