package signing

import (
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/internal/say"
)

const (

	// agreementFileLimit is the largest document taken: a contract, not a scan of a box file.
	AgreementFileLimit = 10 << 20
)

var errNotDraft = say.New("only a draft agreement can be changed")

// ErrTampered is a document that no longer matches the hash it was kept with.
var ErrTampered = say.New("the document doesn't match the SHA-256 it was kept with")

func agreementName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case name == "":
		return "", say.New("an agreement needs a name")
	case utf8.RuneCountInString(name) > 120:
		return "", say.New("an agreement's name is 120 characters at most")
	}
	return name, nil
}

// FileName is a document's name fit to keep and to hand back in a header: its last
// part only, no control characters, and a .pdf at the end.
func FileName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '"' {
			return -1
		}
		return r
	}, name)
	if strings.Trim(name, "./ ") == "" {
		name = "agreement"
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

func Contains(xs []string, x string) bool {
	return slices.Contains(xs, x)
}
