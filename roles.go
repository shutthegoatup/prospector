package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Role is one opening at a company, such as a contract, found for an offering that has
// roles. Each is pursued or junked on its own, and the company's verdict for the
// offering follows from them.
type Role struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OrgID      int64     `gorm:"index:idx_role,priority:1;not null" json:"-"`
	OfferingID int64     `gorm:"index:idx_role,priority:2;not null" json:"offering_id"`
	CompanyID  int64     `gorm:"index:idx_role,priority:3;not null" json:"company_id"`
	Title      string    `gorm:"not null" json:"title"`
	URL        string    `gorm:"not null;default:''" json:"url"`
	Details    string    `gorm:"not null;default:''" json:"details"`
	Stage      string    `gorm:"not null;default:'review'" json:"stage"`
	Reason     string    `gorm:"not null;default:''" json:"reason"`
	By         string    `gorm:"column:decided_by;not null;default:''" json:"by"`
	FoundBy    string    `gorm:"not null;default:''" json:"found_by"`
	TaskID     int64     `gorm:"not null;default:0" json:"task_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

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
		return errors.New("a role needs a title")
	case utf8.RuneCountInString(in.Title) > roleTitleMax:
		return fmt.Errorf("a role's title is %d characters at most", roleTitleMax)
	case utf8.RuneCountInString(in.Details) > roleDetailsMax:
		return fmt.Errorf("a role's details are %d characters at most", roleDetailsMax)
	case in.SourceURL != "" && !httpURL.MatchString(in.SourceURL):
		return errors.New("source_url must be an http(s) link")
	case needSource && in.SourceURL == "":
		return errors.New("a role needs the source_url of the posting or page that shows it")
	}
	return nil
}

// rolesOffering is the offering with this id when it has roles; the organisation's own
// can't, as its wording isn't stored where a switch could be. The caller holds the company.
func rolesOffering(tx *gorm.DB, org, id int64) (*Offering, error) {
	if id == 0 {
		return nil, conflict("roles are kept for an offering that has them; the organisation's own doesn't")
	}
	o, err := heldOffering(tx, org, id)
	if err != nil {
		return nil, err
	}
	if !o.HasRoles {
		return nil, conflict(fmt.Sprintf("%s doesn't have roles: turn them on in its settings first", o.Name))
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
func junkedSaid(r Role) string {
	said := fmt.Sprintf("role %d, %q, was junked by %s on %s", r.ID, r.Title, r.By, r.UpdatedAt.Format("2 Jan 2006"))
	if r.Reason != "" {
		said += ": " + r.Reason
	}
	return said + ". Don't add it again"
}

// addRole adds a role to a company held by the caller, for an offering it has also held.
// A new role brings a company back to review for the offering, unless a person ruled it
// out themselves rather than by junking its roles.
func (s *Store) addRole(tx *gorm.DB, org int64, o *Offering, company int64, in RoleIn, who Actor) (*Role, bool, error) {
	if had, err := sameRole(tx, org, o.ID, company, in); err != nil || had != nil {
		if had != nil && had.Stage == RoleJunked {
			return had, true, conflict(junkedSaid(*had))
		}
		return had, true, err
	}
	if _, err := putUpForReview(tx, org, o.ID, company, who.Name); err != nil {
		return nil, false, err
	}
	var v Verdict
	if err := tx.Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).First(&v).Error; err != nil {
		return nil, false, err
	}
	if v.Stage == CompanyRejected && v.Reason != everyRoleJunked {
		said := fmt.Sprintf("%s rejected this company for %s", v.By, o.Name)
		if v.Reason != "" {
			said += ": " + v.Reason
		}
		return nil, false, conflict(said + ". Don't add its roles")
	}
	r := Role{OrgID: org, OfferingID: o.ID, CompanyID: company, Title: in.Title, URL: in.SourceURL, Details: in.Details,
		Stage: RoleReview, FoundBy: who.Name, TaskID: who.Task}
	if err := tx.Create(&r).Error; err != nil {
		return nil, false, err
	}
	if err := s.logAs(tx, org, 0, &company, who, "add:role", str(o.Name+": "+r.Title)); err != nil {
		return nil, false, err
	}
	return &r, false, s.followRoles(tx, org, o, company, who.Name)
}

// followRoles sets the company's verdict for the offering from its roles: up for review
// while one waits, so it is in a queue someone works, else qualified while one is
// pursued, and ruled out once every one is junked.
func (s *Store) followRoles(tx *gorm.DB, org int64, o *Offering, company int64, actor string) error {
	var stages []string
	if err := tx.Model(&Role{}).Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).Pluck("stage", &stages).Error; err != nil {
		return err
	}
	if len(stages) == 0 {
		return nil
	}
	want, reason := CompanyRejected, everyRoleJunked
	for _, st := range stages {
		switch {
		case st == RoleReview:
			want, reason = CompanyReview, ""
		case st == RolePursued && want != CompanyReview:
			want, reason = CompanyQualified, ""
		}
	}
	var was string
	if err := tx.Model(&Verdict{}).Select("stage").Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).Scan(&was).Error; err != nil {
		return err
	}
	if was == want {
		return nil
	}
	v := Verdict{OrgID: org, OfferingID: o.ID, CompanyID: company, Stage: want, Reason: reason, By: actor, UpdatedAt: time.Now()}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}, {Name: "offering_id"}, {Name: "company_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"stage", "reason", "decided_by", "updated_at"}),
	}).Create(&v).Error; err != nil {
		return err
	}
	said := o.Name
	if reason != "" {
		said += ": " + reason
	}
	return s.log(tx, org, 0, &company, actor, "verdict:"+want, &said)
}

// AddRole is a person adding a role they found to a company, for an offering with roles.
func (s *Store) AddRole(org, offering, company int64, in RoleIn, actor string) (*Role, error) {
	if err := in.check(false); err != nil {
		return nil, err
	}
	var r *Role
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := hold(tx, &Company{}, org, company); err != nil {
			return err
		}
		if _, err := s.companyIn(tx, org, company); err != nil {
			return err
		}
		o, err := rolesOffering(tx, org, offering)
		if err != nil {
			return err
		}
		var had bool
		if r, had, err = s.addRole(tx, org, o, company, in, Actor{Name: actor}); err != nil {
			return err
		}
		if had {
			return conflict(fmt.Sprintf("%s already has this role, as role %d", o.Name, r.ID))
		}
		return nil
	})
	return r, err
}

func (s *Store) companyIn(tx *gorm.DB, org, id int64) (*Company, error) {
	var c Company
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
		return nil, missing(err, "no company %d", id)
	}
	return &c, nil
}

// JudgeRole pursues or junks a role, or puts it back up for review, and brings the
// company's verdict for its offering into line.
func (s *Store) JudgeRole(org, id int64, stage, reason, actor string) (*Role, error) {
	if stage != RoleReview && stage != RolePursued && stage != RoleJunked {
		return nil, fmt.Errorf("unknown stage %q: review, pursued or junked", stage)
	}
	reason = strings.TrimSpace(reason)
	var r Role
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&r).Error; err != nil {
			return missing(err, "no role %d", id)
		}
		if stage != RoleJunked && reason != "" {
			return errors.New("a reason is kept only for a junked role")
		}
		if err := hold(tx, &Company{}, org, r.CompanyID); err != nil {
			return err
		}
		o, err := heldOffering(tx, org, r.OfferingID)
		if err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&r).Error; err != nil {
			return missing(err, "no role %d", id)
		}
		if r.Stage == stage && r.Reason == reason {
			return nil
		}
		r.Stage, r.Reason, r.By = stage, reason, actor
		if err := tx.Model(&r).Select("stage", "reason", "decided_by", "updated_at").Updates(&r).Error; err != nil {
			return err
		}
		said := o.Name + ": " + r.Title
		if reason != "" {
			said += ": " + reason
		}
		if err := s.log(tx, org, 0, &r.CompanyID, actor, "role:"+stage, &said); err != nil {
			return err
		}
		return s.followRoles(tx, org, o, r.CompanyID, actor)
	})
	return &r, err
}

// Roles are a company's roles for every offering, or for one when offering is set.
func (s *Store) Roles(org, company int64, offering *int64) ([]Role, error) {
	if _, err := s.companyIn(s.db, org, company); err != nil {
		return nil, err
	}
	q := s.in(org).Where("company_id = ?", company)
	if offering != nil {
		q = q.Where("offering_id = ?", *offering)
	}
	var out []Role
	return out, q.Order("offering_id, id").Find(&out).Error
}

// RolesAt are the roles for an offering at each of these companies.
func (s *Store) RolesAt(org, offering int64, companies []int64) (map[int64][]Role, error) {
	out := map[int64][]Role{}
	if offering == 0 || len(companies) == 0 {
		return out, nil
	}
	var rows []Role
	if err := s.in(org).Where("offering_id = ? and company_id in ?", offering, companies).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CompanyID] = append(out[r.CompanyID], r)
	}
	return out, nil
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

// AgentAddRole adds a role an agent found for its task's offering. One junked before is
// refused with why, so it isn't brought back.
func (s *Store) AgentAddRole(org int64, who Actor, in AddRoleIn) (*AddRoleOut, error) {
	who, err := s.OnTask(org, who, in.Task)
	if err != nil {
		return nil, err
	}
	if who.Offering == 0 {
		return nil, errors.New("add_role is for a task finding roles for an offering that has them; this isn't one, so add the role as an open_roles fact")
	}
	if err := in.check(true); err != nil {
		return nil, err
	}
	c, err := s.companyRef(org, in.Company)
	if err != nil {
		return nil, err
	}
	var out AddRoleOut
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := hold(tx, &Company{}, org, c.ID); err != nil {
			return err
		}
		o, err := rolesOffering(tx, org, who.Offering)
		if err != nil {
			return err
		}
		stages, err := offeringStages(tx, org, o.ID, []int64{c.ID})
		if err != nil {
			return err
		}
		r, had, err := s.addRole(tx, org, o, c.ID, in.RoleIn, who)
		if err != nil {
			return err
		}
		out = AddRoleOut{ID: r.ID, Existed: had, Stage: r.Stage}
		// A new role is a find, wherever its company stood; one already here isn't.
		if !had {
			return nil
		}
		if err := s.heldWhenFound(tx, org, who, c, stages[c.ID]); err != nil {
			return err
		}
		return s.matched(tx, org, who, 0, &c.ID)
	})
	return &out, err
}

func (s *Store) roleBriefs(org, company int64) ([]RoleBrief, error) {
	var out []RoleBrief
	return out, s.db.Model(&Role{}).Select("roles.id, offerings.name as offering, roles.title, roles.url, roles.details, roles.stage, roles.reason").
		Joins("join offerings on offerings.id = roles.offering_id and offerings.org_id = roles.org_id").
		Where("roles.org_id = ? and roles.company_id = ?", org, company).Order("lower(offerings.name), roles.id").Scan(&out).Error
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
