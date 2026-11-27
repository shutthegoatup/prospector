package main

import (
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Preparing an agreement for signing: who signs it and in what order, who gets a copy,
// and where on its pages each signer's fields go. All of it changes only while the
// agreement is a draft; sending it freezes the lot.

func init() {
	// pdfcpu otherwise writes a configuration directory under the user's home.
	model.ConfigPath = "disable"
}
