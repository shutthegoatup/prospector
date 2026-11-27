package main

import (
	"regexp"
)

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
