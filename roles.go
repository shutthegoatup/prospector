package main

import (
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
)

const (
	RoleReview  = "review"
	RolePursued = "pursued"
	RoleJunked  = "junked"

	// everyRoleJunked is the reason on a verdict followRoles ruled out, which a new role
	// may undo; a rejection a person made stands.
	everyRoleJunked = "every role junked"

	roleTitleMax   = 200
	roleDetailsMax = 300
)

// RoleIn is a role as someone adds it.
type RoleIn struct {
	Title     string `json:"title" jsonschema:"the role as the posting names it, such as Lead platform engineer (contract, 6 months)"`
	SourceURL string `json:"source_url" jsonschema:"the posting, or the page that shows the role is open"`
	Details   string `json:"details,omitempty" jsonschema:"one line: day rate or salary, length, IR35, location and remote split, clearance, as the posting says them"`
}

func (in *RoleIn) check(needSource bool) error {
	in.Title, in.SourceURL, in.Details = strings.TrimSpace(in.Title), strings.TrimSpace(in.SourceURL), strings.Join(strings.Fields(in.Details), " ")
	switch {
	case in.Title == "":
		return say.New("a role needs a title")
	case utf8.RuneCountInString(in.Title) > roleTitleMax:
		return say.Errorf("a role's title is %d characters at most", roleTitleMax)
	case utf8.RuneCountInString(in.Details) > roleDetailsMax:
		return say.Errorf("a role's details are %d characters at most", roleDetailsMax)
	case in.SourceURL != "" && !httpURL.MatchString(in.SourceURL):
		return say.New("source_url must be an http(s) link")
	case needSource && in.SourceURL == "":
		return say.New("a role needs the source_url of the posting or page that shows it")
	}
	return nil
}

// rolesOffering is the offering with this id when it has roles; the organisation's own
// can't, as its wording isn't stored where a switch could be. The caller holds the company.
func rolesOffering(tx *gorm.DB, org, id int64) (*Offering, error) {
	if id == 0 {
		return nil, conflict{Text: say.New("roles are kept for an offering that has them; the organisation's own doesn't")}
	}
	o, err := heldOffering(tx, org, id)
	if err != nil {
		return nil, err
	}
	if !o.HasRoles {
		return nil, conflict{Text: say.Errorf("%s doesn't have roles: turn them on in its settings first", o.Name)}
	}
	return o, nil
}

// sameRole is a role at the company already, for the offering: the same posting, the
// same title as a junked one, or the same title when either has no posting.
func sameRole(tx *gorm.DB, org, offering, company int64, in RoleIn) (*Role, error) {
	var roles []Role
	if err := tx.Where("org_id = ? and offering_id = ? and company_id = ?", org, offering, company).Order("id").Find(&roles).Error; err != nil {
		return nil, err
	}
	at, title := normalURL(in.SourceURL), sameWords(in.Title)
	for _, r := range roles {
		if at != "" && normalURL(r.URL) == at {
			return &r, nil
		}
	}
	// A junked role found again on another page is still junked.
	for _, r := range roles {
		if sameWords(r.Title) == title && (r.Stage == RoleJunked || at == "" || r.URL == "") {
			return &r, nil
		}
	}
	return nil, nil
}

func sameWords(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// junkedSaid is what an agent is told when it brings back a role that was junked.
func junkedSaid(r Role) *say.Text {
	day := r.UpdatedAt.Format("2 Jan 2006")
	if r.Reason != "" {
		return say.Errorf("role %d, %q, was junked by %s on %s: %s. Don't add it again", r.ID, r.Title, r.By, day, r.Reason)
	}
	return say.Errorf("role %d, %q, was junked by %s on %s. Don't add it again", r.ID, r.Title, r.By, day)
}

// AddRoleIn is add_role's input.
type AddRoleIn struct {
	Company string `json:"company" jsonschema:"company id or exact name; add the company first if it isn't here"`
	RoleIn
	Task int64 `json:"task,omitempty" jsonschema:"the id of the task this is for"`
}

type AddRoleOut struct {
	ID      int64  `json:"id"`
	Existed bool   `json:"existed" jsonschema:"true if the company already had this role for the offering; nothing was added"`
	Stage   string `json:"stage" jsonschema:"the role's stage: review, pursued or junked"`
}

// judgedByRoles says whether a company's verdict for an offering follows its roles there,
// so a person can't set it by hand.
func judgedByRoles(tx *gorm.DB, org int64, o *Offering, company int64) (bool, error) {
	if !o.HasRoles {
		return false, nil
	}
	var n int64
	err := tx.Model(&Role{}).Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).Count(&n).Error
	return n > 0, err
}
