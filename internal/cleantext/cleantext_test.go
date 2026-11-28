package cleantext

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestOneLine(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"  Larkmoor\n\tYachts  ": "Larkmoor Yachts", "": "", "one": "one", "a \r\n b": "a b"} {
		if got := OneLine(in); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNULEscapesAreDroppedFromJSON(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`{"name":"Acme\u0000 Boats"}`:      `{"name":"Acme Boats"}`,
		`{"name":"\u0000\u0000"}`:          `{"name":""}`,
		`{"path":"C:\\u0000"}`:             `{"path":"C:\\u0000"}`,
		`{"note":"say \"\u0000\" \u00e9"}`: `{"note":"say \"\" \u00e9"}`,
		`{"name":"Acme\u0001"}`:            `{"name":"Acme\u0001"}`,
		`{"cut":"\u00`:                     `{"cut":"\u00`,
		`{"a":"x\\","b":"\u0000y"}`:        `{"a":"x\\","b":"y"}`,
	} {
		// One byte at a time, so every escape is split across reads somewhere.
		r := &withoutNUL{src: io.NopCloser(iotest.OneByteReader(strings.NewReader(in)))}
		got, err := io.ReadAll(r)
		if err != nil || string(got) != want {
			t.Errorf("%s: %s, %v; want %s", in, got, err, want)
		}
	}
}
