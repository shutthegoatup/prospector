package main

import (
	"bytes"
	"context"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"github.com/shutthegoatup/prospector/internal/signing"
)

// Sealing an agreement once everyone has signed: what each signer filled in is drawn onto
// the pages they saw, a certificate of what happened is added as the last page, and the
// whole is signed with the organisation's own key, so any later change to it shows.

// stampPDF draws each stamp onto its page, as fillIn does, and writes the document out.
func stampPDF(doc []byte, stamps []signing.Stamp) ([]byte, error) {
	rc, err := signing.ReadForSealing(doc)
	if err != nil {
		return nil, err
	}
	if err := signing.FillIn(rc, stamps); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := api.WriteContext(context.Background(), rc, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
