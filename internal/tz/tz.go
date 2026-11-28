package tz

import (
	"regexp"
	"strings"

	"github.com/nyaruka/phonenumbers/v2"
	"github.com/nyaruka/phonenumbers/v2/timezone"
)

var countryTag = regexp.MustCompile(`\(([A-Z]{2})(?:/[A-Z]{2})?[,)]`)

// Guess uses libphonenumber's time zone data for the first phone number, else the
// (XX) country tag in the maker name. Where a number or country spans several zones,
// the zone of libphonenumber's example number for that country breaks the tie.
func Guess(phone, maker string) *string {
	first, _, _ := strings.Cut(phone, "/")
	if n, err := phonenumbers.Parse(first, ""); err == nil && phonenumbers.IsPossibleNumber(n) {
		// Mobile and toll-free ranges outside North America say nothing about where the person is.
		switch phonenumbers.GetNumberType(n) {
		case phonenumbers.MOBILE, phonenumbers.TOLL_FREE, phonenumbers.UAN, phonenumbers.VOIP:
		default:
			if zones, err := timezone.GetTimeZonesForNumber(n); err == nil && len(zones) == 1 && zones[0] != timezone.Unknown {
				return &zones[0]
			}
		}
		if tz := regionTZ(phonenumbers.GetRegionCodeForNumber(n)); tz != nil {
			return tz
		}
	}
	if m := countryTag.FindStringSubmatch(maker); m != nil {
		region := m[1]
		if region == "UK" {
			region = "GB"
		}
		return regionTZ(region)
	}
	return nil
}

func regionTZ(region string) *string {
	example := phonenumbers.GetExampleNumber(region)
	if example == nil {
		return nil
	}
	zones, err := timezone.GetTimeZonesForNumber(example)
	if err != nil || len(zones) == 0 || zones[0] == timezone.Unknown {
		return nil
	}
	return &zones[0]
}
