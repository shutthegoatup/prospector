package signing

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/mail"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const RecipientSigner = "signer"

const RecipientCopy = "copy"

const // agreementRecipientLimit and agreementFieldLimit bound what one draft holds.
agreementRecipientLimit = 20

const agreementFieldLimit = 200

// fieldKinds are what a signer can be asked to fill in. Signature is the one every
// signer needs before an agreement can go.
var fieldKinds = []string{"signature", "initials", "name", "date", "text", "tick"}

// Party is an agreement recipient with who they are now: a contact's or member's current name
// and address, which sending will freeze.
type Party struct {
	store.AgreementRecipient
	Name  string `json:"name"`
	Email string `json:"email"`
}

// pdfPages is how many pages a document has, refusing one Prospector couldn't later fill
// in and seal: encrypted, already signed, or one it can't read at all. It is read, not
// validated: real PDFs often break a rule of their own version that doesn't matter here.
func pdfPages(data []byte) (int, error) {
	ctx, err := api.ReadContext(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		if errors.Is(err, pdfcpu.ErrWrongPassword) || strings.Contains(err.Error(), "password") {
			return 0, say.New("that PDF is password-protected: save it without a password first")
		}
		return 0, say.Errorf("that PDF can't be read to place fields on it: %v", err)
	}
	if ctx.XRefTable.Encrypt != nil || ctx.E != nil { //nolint:staticcheck // the xref table's, unlike ctx.E
		return 0, say.New("that PDF is encrypted: save it without protection first")
	}
	if signedPDF(ctx.XRefTable, data) {
		return 0, say.New("that PDF is already signed: upload the unsigned version")
	}
	if err := ctx.EnsurePageCount(); err != nil {
		return 0, say.Errorf("that PDF can't be read to place fields on it: %v", err)
	}
	if ctx.PageCount < 1 {
		return 0, say.New("that PDF has no pages")
	}
	return ctx.PageCount, nil
}

// signedPDF is whether a document carries a signature: its form says so, or a signature's
// byte range is in it, which sealing it again would break.
func signedPDF(x *model.XRefTable, data []byte) bool {
	if bytes.Contains(data, []byte("/ByteRange")) {
		return true
	}
	root, err := x.Catalog()
	if err != nil {
		return false
	}
	form, err := x.DereferenceDict(root["AcroForm"])
	if err != nil || form == nil {
		return false
	}
	flags := form.IntEntry("SigFlags")
	return flags != nil && *flags&1 != 0
}

// signable is whether a contact may be sent an agreement for a company: a person, picked,
// at that company, with one address, and not on the never-contact list.
func signable(o *store.Org, company int64, c store.Contact) error {
	switch {
	case strings.HasPrefix(c.Key, "org:"):
		return say.New("that row stands for a company, not a person")
	case c.CompanyID == nil || *c.CompanyID != company:
		return say.New("that person is not at the agreement's company")
	case c.Status == store.ContactSuggested:
		return say.New("this contact is still a suggestion in Research; a person picks them first")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(store.Deref(c.Email))); err != nil {
		return say.New("they have no single email address to send the agreement to")
	}
	return o.Settings.Listed(store.Deref(c.Email), store.Deref(c.Linkedin))
}

// fieldFits is whether a field is one a signer can fill in: a kind there is, for one of
// the agreement's signers, big enough to use and wholly on a page it has.
func fieldFits(f store.AgreementField, pages int, signers map[int64]bool) error {
	const least = 0.01
	switch {
	case !signers[f.RecipientID]:
		return say.Errorf("recipient %d isn't one of this agreement's signers", f.RecipientID)
	case !Contains(fieldKinds, f.Kind):
		return say.Errorf("a field is one of %s", strings.Join(fieldKinds, ", "))
	case f.Page < 1 || f.Page > pages:
		return say.Errorf("the document has pages 1 to %d", pages)
	}
	for _, v := range []float64{f.X, f.Y, f.W, f.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return say.New("a field's place is four numbers between 0 and 1")
		}
	}
	if f.W < least || f.H < least || f.X < 0 || f.Y < 0 || f.X+f.W > 1+1e-9 || f.Y+f.H > 1+1e-9 {
		return say.New("a field has to lie wholly on its page")
	}
	return nil
}

func orElse(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
