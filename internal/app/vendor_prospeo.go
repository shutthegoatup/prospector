package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
)

// prospeo is Prospeo's Enrich Person: a name and a company's domain, or a LinkedIn
// profile, give a work email, a mobile and the profile.
type prospeo struct{}

func init() { prospecting.Vendors["prospeo"] = prospeo{} }

func (prospeo) Name() string     { return "Prospeo" }
func (prospeo) Fields() []string { return []string{"email", "phone", "linkedin"} }

func (prospeo) Find(ctx context.Context, c *http.Client, base, key string, p prospecting.EnrichPerson, field string) (prospecting.VendorFind, error) {
	if base == "" {
		base = "https://api.prospeo.io"
	}
	data := map[string]string{}
	if p.Linkedin != "" {
		data["linkedin_url"] = p.Linkedin
	}
	if name := strings.TrimSpace(p.Name); name != "" {
		data["full_name"] = name
		switch {
		case p.Domain != "":
			data["company_website"] = p.Domain
		case p.Company != "":
			data["company_name"] = p.Company
		}
	}
	if data["linkedin_url"] == "" && data["company_website"] == "" && data["company_name"] == "" {
		return prospecting.VendorFind{}, say.New("it needs the person's name and company, or their LinkedIn profile")
	}
	ask := map[string]any{"data": data}
	if field == "phone" {
		// A mobile costs ten times an email, and nothing when there's none to find.
		ask["only_verified_mobile"] = true
	}
	raw, _ := json.Marshal(ask)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/enrich-person", bytes.NewReader(raw))
	if err != nil {
		return prospecting.VendorFind{}, err
	}
	req.Header.Set("X-KEY", key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return prospecting.VendorFind{}, err
	}
	defer res.Body.Close()
	var body struct {
		Error     bool   `json:"error"`
		ErrorCode string `json:"error_code"`
		Person    *struct {
			LinkedinURL string `json:"linkedin_url"`
			Email       *struct {
				Status   string `json:"status"`
				Revealed bool   `json:"revealed"`
				Email    string `json:"email"`
			} `json:"email"`
			Mobile *struct {
				Status   string `json:"status"`
				Revealed bool   `json:"revealed"`
				Mobile   string `json:"mobile"`
			} `json:"mobile"`
		} `json:"person"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return prospecting.VendorFind{}, say.Errorf("an answer that isn't JSON (%s)", res.Status)
	}
	if body.Error || res.StatusCode != http.StatusOK {
		if body.ErrorCode == "NO_MATCH" {
			return prospecting.VendorFind{}, nil
		}
		why := body.ErrorCode
		if why == "" {
			why = res.Status
		}
		return prospecting.VendorFind{}, errors.New(prospeoErrors[why] + why)
	}
	if body.Person == nil {
		return prospecting.VendorFind{}, nil
	}
	// Only what was paid for is shown in full; anything else is masked with stars.
	shown := func(revealed bool, v string) string {
		if !revealed || strings.Contains(v, "*") {
			return ""
		}
		return strings.TrimSpace(v)
	}
	switch field {
	case "email":
		e := body.Person.Email
		if e == nil {
			return prospecting.VendorFind{}, nil
		}
		status := "guess"
		if e.Status == "VERIFIED" {
			status = "verified"
		}
		if v := shown(e.Revealed, e.Email); v != "" {
			return prospecting.VendorFind{Value: v, EmailStatus: status}, nil
		}
	case "phone":
		if m := body.Person.Mobile; m != nil && m.Status == "VERIFIED" {
			return prospecting.VendorFind{Value: shown(m.Revealed, m.Mobile)}, nil
		}
	case "linkedin":
		return prospecting.VendorFind{Value: strings.TrimSpace(body.Person.LinkedinURL)}, nil
	}
	return prospecting.VendorFind{}, nil
}

var prospeoErrors = map[string]string{
	"INVALID_API_KEY":      "the key was refused: ",
	"INSUFFICIENT_CREDITS": "the account is out of credits: ",
	"INVALID_DATAPOINTS":   "not enough to go on: ",
}
