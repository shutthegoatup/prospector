package say

import "testing"

func TestWordsReadInEnglishAsErrorsDo(t *testing.T) {
	for _, c := range []struct {
		text *Text
		want string
	}{
		{New("100% of it, as written"), "100% of it, as written"},
		{Errorf("no company %d", 12345), "no company 12345"},
		{Errorf("too many attempts: try again in %d seconds", 1), "too many attempts: try again in 1 second"},
		{Errorf("too many attempts: try again in %d seconds", 30), "too many attempts: try again in 30 seconds"},
		{Errorf("%s has %d roles, so it keeps them", "Lark", 1), "Lark has 1 role, so it keeps it"},
		{Errorf("not catalogued %q", "x"), `not catalogued "x"`},
	} {
		if got := c.text.Error(); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

func TestAPseudoLocaleMarksWhatWasTranslated(t *testing.T) {
	Pseudo("en-XA")
	if got := Errorf("no company %d", 7).In("en-XA"); got != "[ñö çöɱþáñý 7]" {
		t.Errorf("a format's verbs are filled as in English: %q", got)
	}
	inner := Errorf("too many attempts: try again in %d seconds", 2)
	if got := Errorf("%s", inner).In("en-XA"); got != "[ţöö ɱáñý áţţéɱþţš: ţŕý áĝáîñ îñ 2 šéçöñðš]" {
		t.Errorf("words inside words are in the same language: %q", got)
	}
	if got := Errorf("not catalogued %d", 1).In("en-XA"); got != "not catalogued 1" {
		t.Errorf("what the catalogue lacks reads as written: %q", got)
	}
}

func TestAFormatSplitsIntoTextAndVerbs(t *testing.T) {
	got := Split("%[2]s has %d%% of %-5.2f")
	want := []string{"%[2]s", " has ", "%d", "%%", " of ", "%-5.2f"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%q, want %q", got, want)
		}
	}
}
