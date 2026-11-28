package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
)

// hunter is Hunter.io's Email Finder: a name and a company's domain give the most
// likely work address, with Hunter's own check of it.
type hunter struct{}

func init() { prospecting.Vendors["hunter"] = hunter{} }

func (hunter) Name() string     { return "Hunter" }
func (hunter) Fields() []string { return []string{"email"} }

func (hunter) Find(ctx context.Context, c *http.Client, base, key string, p prospecting.EnrichPerson, field string) (prospecting.VendorFind, error) {
	if base == "" {
		base = "https://api.hunter.io"
	}
	q := url.Values{}
	switch {
	case p.Domain != "":
		q.Set("domain", p.Domain)
	case p.Company != "":
		q.Set("company", p.Company)
	default:
		return prospecting.VendorFind{}, say.New("it needs the company's domain or name")
	}
	if strings.TrimSpace(p.Name) == "" {
		return prospecting.VendorFind{}, say.New("it needs the person's name")
	}
	q.Set("full_name", p.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v2/email-finder?"+q.Encode(), nil)
	if err != nil {
		return prospecting.VendorFind{}, err
	}
	// In a header rather than the address, so it stays out of any proxy's log.
	req.Header.Set("X-API-KEY", key)
	res, err := c.Do(req)
	if err != nil {
		return prospecting.VendorFind{}, err
	}
	defer res.Body.Close()
	var body struct {
		Data struct {
			Email        *string `json:"email"`
			Verification struct {
				Status string `json:"status"`
			} `json:"verification"`
		} `json:"data"`
		Errors []struct {
			ID      string `json:"id"`
			Details string `json:"details"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return prospecting.VendorFind{}, say.Errorf("an answer that isn't JSON (%s)", res.Status)
	}
	if res.StatusCode == http.StatusNotFound {
		return prospecting.VendorFind{}, nil
	}
	if res.StatusCode != http.StatusOK {
		why := res.Status
		if len(body.Errors) > 0 {
			why = strings.TrimSpace(body.Errors[0].Details)
		}
		return prospecting.VendorFind{}, errors.New(why)
	}
	if body.Data.Email == nil {
		return prospecting.VendorFind{}, nil
	}
	status := "guess"
	if body.Data.Verification.Status == "valid" {
		status = "verified"
	}
	return prospecting.VendorFind{Value: *body.Data.Email, EmailStatus: status}, nil
}
