package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/tz"
)

// researchStore is the research area's queries: what the organisation offers and the
// roles it looks for, the tasks agents are handed, and what agents add while working them.
type researchStore struct {
	*core
	// What agents add are companies and people, an organisation's settings say who is
	// left out, and an address is only kept where its domain takes mail.
	prospecting *prospectingStore
	identity    *identityStore
	outreach    *outreachStore
}

func (s *researchStore) CreateTask(org int64, title, brief, by string) (*Task, error) {
	return s.CreateTaskFor(org, 0, title, brief, by)
}

// CreateTaskFor starts a task finding companies for an offering; 0 is the organisation's own.
func (s *researchStore) CreateTaskFor(org, offering int64, title, brief, by string) (*Task, error) {
	return s.CreateTaskWanting(org, offering, 0, title, brief, by)
}

// CreateTaskWanting is CreateTaskFor asking for about want companies.
func (s *researchStore) CreateTaskWanting(org, offering int64, want int, title, brief, by string) (*Task, error) {
	if want < 0 || want > maxWant {
		return nil, say.Errorf("ask for between 1 and %d companies, or leave the number out", maxWant)
	}
	brief, err := words(brief, maxBrief, "the brief")
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = firstLine(brief)
	}
	if r := []rune(title); len(r) > 60 {
		title = strings.TrimSpace(string(r[:59])) + "…"
	}
	t := Task{OrgID: org, Title: title, CreatedBy: by, OfferingID: offering, Want: want}
	return &t, s.DB.Transaction(func(tx *gorm.DB) error {
		if offering != 0 {
			if _, err := heldOffering(tx, org, offering); err != nil {
				return err
			}
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if err := tx.Create(&TaskRequest{OrgID: org, TaskID: t.ID, Text: brief, AskedBy: by, AskedAt: time.Now()}).Error; err != nil {
			return err
		}
		return s.LogAs(tx, org, 0, nil, Actor{Name: by, Task: t.ID}, "task:new", nil)
	})
}

// OnTask ties what an agent is about to do to a task, if it named one.
func (s *researchStore) OnTask(org int64, who Actor, task int64) (Actor, error) {
	if task == 0 {
		return who, nil
	}
	t, err := openTask(s.DB, org, task)
	if err != nil {
		return who, err
	}
	// Only a release from before offerings could have removed one an open task is for.
	if _, err := s.offeringOf(org, t.OfferingID); err != nil {
		var gone notFound
		if errors.As(err, &gone) {
			return who, conflict{say.Errorf("the offering task %d was finding companies for has been removed: stop, and leave a note saying so", task)}
		}
		return who, err
	}
	who.Task, who.Offering = task, t.OfferingID
	return who, s.renewClaims(s.DB, org, task, who, time.Now())
}

// matched records that something already here answers a task. Without it a company
// known before the task began would never count as found, and every agent would
// find it again.
func (s *researchStore) matched(tx *gorm.DB, org int64, who Actor, contact int64, company *int64) error {
	if who.Task == 0 {
		return nil
	}
	// A note about a company doesn't make it a result.
	q := tx.Model(&Event{}).Where("org_id = ? and task_id = ? and company_id = ? and action not like 'note:%'", org, who.Task, company)
	if contact != 0 {
		q = q.Where("contact_id = ?", contact)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil || n > 0 {
		return err
	}
	return s.LogAs(tx, org, contact, company, who, "task:match", nil)
}

func (s *researchStore) AskTask(org, task int64, text, by string) (*TaskRequest, error) {
	text, err := words(text, maxRequest, "the request")
	if err != nil {
		return nil, err
	}
	r := &TaskRequest{OrgID: org, TaskID: task, Text: text, AskedBy: by, AskedAt: time.Now()}
	return r, s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := openTask(tx, org, task); err != nil {
			return err
		}
		if err := tx.Create(r).Error; err != nil {
			return err
		}
		return s.LogAs(tx, org, 0, nil, Actor{Name: by, Task: task}, "task:ask", nil)
	})
}

// TaskByID and RequestByID are one task, or one request, of the organisation's.
func (s *researchStore) TaskByID(org, id int64) (*Task, error) {
	var t Task
	if err := s.DB.Where("org_id = ? and id = ?", org, id).First(&t).Error; err != nil {
		return nil, missing(err, "no task %d in this organisation", id)
	}
	return &t, nil
}

func (s *researchStore) RequestByID(org, id int64) (*TaskRequest, error) {
	var r TaskRequest
	if err := s.DB.Where("org_id = ? and id = ?", org, id).First(&r).Error; err != nil {
		return nil, missing(err, "no request %d in this organisation", id)
	}
	return &r, nil
}

func (s *researchStore) setRequestDone(org, request int64, done bool, who Actor) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var r TaskRequest
		if err := tx.Where("org_id = ? and id = ?", org, request).First(&r).Error; err != nil {
			return missing(err, "no request %d in this organisation", request)
		}
		if _, err := openTask(tx, org, r.TaskID); err != nil {
			return err
		}
		if done == (r.DoneAt != nil) {
			return nil
		}
		updates, action := map[string]any{"done_at": nil, "done_by": ""}, "task:reopen"
		if done {
			updates, action = map[string]any{"done_at": time.Now(), "done_by": who.Name}, "task:done"
		}
		// Only a request still as it was read is changed, so two finishing it at once record it once.
		was := "done_at is not null"
		if done {
			was = "done_at is null"
		}
		changed := tx.Model(&TaskRequest{}).Where("org_id = ? and id = ? and "+was, org, r.ID).Updates(updates)
		if changed.Error != nil || changed.RowsAffected == 0 {
			return changed.Error
		}
		// A finished request has nothing left to keep anyone off.
		if err := tx.Where("org_id = ? and request_id = ?", org, r.ID).Delete(&RequestClaim{}).Error; err != nil {
			return err
		}
		who.Task = r.TaskID
		return s.LogAs(tx, org, 0, nil, who, action, nil)
	})
}

func (s *researchStore) SetRequestDone(org, request int64, done bool, by string) error {
	return s.setRequestDone(org, request, done, Actor{Name: by})
}

func (s *researchStore) SetTaskClosed(org, task int64, closed bool, by string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var t Task
		if err := tx.Where("org_id = ? and id = ?", org, task).First(&t).Error; err != nil {
			return missing(err, "no task %d in this organisation", task)
		}
		if closed == (t.ClosedAt != nil) {
			return nil
		}
		if !closed && t.OfferingID != 0 {
			if _, err := heldOffering(tx, org, t.OfferingID); err != nil {
				var gone notFound
				if errors.As(err, &gone) {
					return conflict{say.New("the offering this task was finding companies for has been removed: start a new task instead")}
				}
				return err
			}
		}
		var at *time.Time
		action := "task:open"
		if closed {
			now := time.Now()
			at, action = &now, "task:close"
		}
		if err := tx.Model(&t).Update("closed_at", at).Error; err != nil {
			return err
		}
		return s.LogAs(tx, org, 0, nil, Actor{Name: by, Task: task}, action, nil)
	})
}

func (s *researchStore) DeleteNote(org, id int64) error {
	res := s.InOrg(org).Where("id = ?", id).Delete(&TaskNote{})
	if res.Error == nil && res.RowsAffected == 0 {
		return noSuch("no note %d", id)
	}
	return res.Error
}

// TaskNotesPage is one page of the notes agents left on a task, newest first, each
// with the name of the company it is about.
func (s *researchStore) TaskNotesPage(org, task int64, page int) ([]NoteView, Page, error) {
	theirs := func() *gorm.DB { return s.InOrg(org).Model(&TaskNote{}).Where("task_id = ?", task) }
	var total int64
	if err := theirs().Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, taskNotesPageSize, int(total))
	var notes []TaskNote
	if err := theirs().Order("id desc").Limit(pg.Size).Offset(pg.offset()).Find(&notes).Error; err != nil {
		return nil, Page{}, err
	}
	out, err := s.noteViews(org, notes)
	return out, pg, err
}

// TaskNotes is TaskNotesPage a cursor at a time, for the API.
func (s *researchStore) TaskNotes(org, task int64, page paging.Page) ([]NoteView, string, error) {
	at, err := page.At(1)
	if err != nil {
		return nil, "", err
	}
	q := s.InOrg(org).Where("task_id = ?", task).Order("id desc").Limit(page.Size() + 1)
	if at[0] > 0 {
		q = q.Where("id < ?", at[0])
	}
	var notes []TaskNote
	if err := q.Find(&notes).Error; err != nil {
		return nil, "", err
	}
	var next string
	if len(notes) > page.Size() {
		notes = notes[:page.Size()]
		next = paging.Cursor(notes[len(notes)-1].ID)
	}
	out, err := s.noteViews(org, notes)
	return out, next, err
}

// noteViews names the company each note is about.
func (s *researchStore) noteViews(org int64, notes []TaskNote) ([]NoteView, error) {
	names := map[int64]string{}
	for _, n := range notes {
		if n.CompanyID != 0 {
			names[n.CompanyID] = ""
		}
	}
	if len(names) > 0 {
		var companies []Company
		if err := s.InOrg(org).Where("id in ?", slices.Collect(maps.Keys(names))).Find(&companies).Error; err != nil {
			return nil, err
		}
		for _, c := range companies {
			names[c.ID] = c.Name
		}
	}
	out := make([]NoteView, len(notes))
	for i, n := range notes {
		out[i] = NoteView{TaskNote: n, Company: names[n.CompanyID]}
	}
	return out, nil
}

// CompanyNotesPage is one page of the notes agents left about a company, newest first.
func (s *researchStore) CompanyNotesPage(org, company int64, page int) ([]TaskNote, Page, error) {
	theirs := func() *gorm.DB { return s.InOrg(org).Model(&TaskNote{}).Where("company_id = ?", company) }
	var total int64
	if err := theirs().Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, companyNotesPageSize, int(total))
	var notes []TaskNote
	return notes, pg, theirs().Order("id desc").Limit(pg.Size).Offset(pg.offset()).Find(&notes).Error
}

// Runs is a page of the organisation's agent work, newest first: the limit runs that
// ended before before, or the latest when before is zero. Runs aren't stored; they are
// read from events, so the page after is the one before the last run on this one, and
// next is zero when there are no more.
func (s *researchStore) Runs(org int64, before time.Time, limit int) (runs []Run, titles map[int64]string, next time.Time) {
	agents := func() *gorm.DB {
		q := s.InOrg(org).Model(&Event{}).Where(`"user" like 'agent:%'`)
		if !before.IsZero() {
			q = q.Where("at < ?", before)
		}
		return q
	}
	var events []Event
	_ = logged(agents().Order("at desc, id desc").Limit(runsRead).Find(&events))
	full := len(events) == runsRead
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	runs = runsOf(events)
	if !before.IsZero() {
		// A run the page before showed began before it, and its start is here too: it is
		// left out, by how soon after it that agent did something on the page before.
		// Read as rows, one an agent.
		cut := map[string]time.Time{}
		for _, r := range runs {
			key := fmt.Sprintf("%d\x00%s", r.KeyID, r.Actor)
			if _, asked := cut[key]; asked {
				continue
			}
			var first Event
			_ = logged(s.InOrg(org).Where(`key_id = ? and "user" = ? and at >= ?`, r.KeyID, r.Actor, before).Order("at, id").Limit(1).Find(&first))
			cut[key] = first.At
		}
		runs = slices.DeleteFunc(runs, func(r Run) bool {
			first := cut[fmt.Sprintf("%d\x00%s", r.KeyID, r.Actor)]
			return !first.IsZero() && first.Sub(r.End) < runGap
		})
	}
	// The page is the newest runs, newest first. It stops before one that may have begun
	// earlier than was read, which is whole on a later page, unless that run is the
	// newest, which is then shown cut: one run of more events than are read.
	cutShort := func(r Run) bool { return full && r.Start.Sub(events[0].At) < runGap }
	shown := 0
	for shown < len(runs) && shown < limit {
		if cutShort(runs[shown]) && shown > 0 {
			break
		}
		shown++
		if cutShort(runs[shown-1]) {
			break
		}
	}
	// Never between two that ended together: the next page starts before that moment,
	// which would leave the one not shown out of both.
	for shown < len(runs) && shown > 0 && runs[shown].End.Equal(runs[shown-1].End) {
		back := shown - 1
		for back > 0 && runs[back-1].End.Equal(runs[shown].End) {
			back--
		}
		if back > 0 {
			shown = back
			break
		}
		for shown < len(runs) && runs[shown].End.Equal(runs[0].End) {
			shown++
		}
	}
	more := shown < len(runs) || full
	runs = runs[:shown]
	switch {
	case more && len(runs) > 0:
		next = runs[len(runs)-1].End
	case full:
		// Every run read was one already shown, longer than a page reads: carry on past it.
		next = events[0].At
	}
	var ids []int64
	for _, r := range runs {
		ids = append(ids, r.Tasks...)
	}
	titles = map[int64]string{}
	if len(ids) > 0 {
		var tasks []Task
		_ = logged(s.InOrg(org).Where("id in ?", ids).Find(&tasks))
		for _, t := range tasks {
			titles[t.ID] = t.Title
		}
	}
	return runs, titles, next
}

// taskViews reads what has come of several tasks of one organisation together: the
// same half-dozen queries for two hundred tasks as for one.
func (s *researchStore) taskViews(tx *gorm.DB, org int64, tasks []Task, now time.Time) ([]TaskView, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	theirs := func() *gorm.DB { return tx.Where("org_id = ? and task_id in ?", org, ids) }
	var requests []TaskRequest
	if err := theirs().Order("id").Find(&requests).Error; err != nil {
		return nil, err
	}
	var events []Event
	if err := theirs().Order("at, id").Find(&events).Error; err != nil {
		return nil, err
	}
	var notes []TaskNote
	if err := theirs().Order("id desc").Find(&notes).Error; err != nil {
		return nil, err
	}
	var held []RequestClaim
	if err := theirs().Where("expires_at > ?", now.UnixMilli()).Find(&held).Error; err != nil {
		return nil, err
	}
	claims := map[int64]RequestClaim{}
	for _, c := range held {
		claims[c.RequestID] = c
	}
	requestsOf, eventsOf, notesOf := map[int64][]TaskRequest{}, map[int64][]Event{}, map[int64][]TaskNote{}
	for _, r := range requests {
		requestsOf[r.TaskID] = append(requestsOf[r.TaskID], r)
	}
	for _, e := range events {
		eventsOf[e.TaskID] = append(eventsOf[e.TaskID], e)
	}
	for _, n := range notes {
		if len(notesOf[n.TaskID]) < taskNotesShown {
			notesOf[n.TaskID] = append(notesOf[n.TaskID], n)
		}
	}
	// Found through the events and notes in the database, not by listing their ids: a
	// busy organisation's would be more than one query can name.
	inEvents := func(column string) *gorm.DB {
		return tx.Model(&Event{}).Select(column).Where("org_id = ? and task_id in ?", org, ids)
	}
	inNotes := tx.Model(&TaskNote{}).Select("company_id").Where("org_id = ? and task_id in ?", org, ids)
	companies, people := map[int64]Company{}, map[int64]Contact{}
	var found []Company
	if err := tx.Where("org_id = ? and (id in (?) or id in (?))", org, inEvents("company_id"), inNotes).Find(&found).Error; err != nil {
		return nil, err
	}
	for _, c := range found {
		companies[c.ID] = c
	}
	var met []Contact
	if err := tx.Where("org_id = ? and id in (?) and key not like 'org:%'", org, inEvents("contact_id")).Find(&met).Error; err != nil {
		return nil, err
	}
	for _, p := range met {
		people[p.ID] = p
	}

	out := make([]TaskView, len(tasks))
	o, err := s.identity.OrgByID(org)
	if err != nil {
		return nil, err
	}
	for i, t := range tasks {
		v := &out[i]
		v.Task = t
		events := eventsOf[t.ID]
		companySince, personSince, heldAs := map[int64]time.Time{}, map[int64]time.Time{}, map[int64]string{}
		v.Withheld = map[string]int{}
		for j, e := range events {
			if strings.HasPrefix(e.User, "agent:") {
				v.Last = &events[j]
			}
			// A note about a company isn't a find: it is often why the company was passed over.
			if strings.HasPrefix(e.Action, "note:") {
				continue
			}
			if e.Action == actionWithheld && e.Value != nil {
				v.Withheld[*e.Value]++
			}
			if e.Action == actionHeld && e.CompanyID != nil && e.Value != nil {
				heldAs[*e.CompanyID] = *e.Value
			}
			if e.CompanyID != nil {
				if _, seen := companySince[*e.CompanyID]; !seen {
					companySince[*e.CompanyID] = e.At
				}
			}
			if e.ContactID != 0 {
				if _, seen := personSince[e.ContactID]; !seen {
					personSince[e.ContactID] = e.At
				}
			}
		}
		for _, id := range slices.Sorted(maps.Keys(companySince)) {
			if c, here := companies[id]; here {
				v.Companies = append(v.Companies, TaskCompany{ID: c.ID, Name: c.Name, Stage: c.Stage, Since: companySince[id], Held: heldAs[id]})
				if o.Settings.CoversCompany(c.Domain) {
					v.Companies[len(v.Companies)-1].Held = heldNeverContact
				}
			}
		}
		// A task for an offering judges its companies by that offering's verdicts.
		if t.OfferingID != 0 {
			stages, err := offeringStages(tx, org, t.OfferingID, slices.Collect(maps.Keys(companySince)))
			if err != nil {
				return nil, err
			}
			for j := range v.Companies {
				v.Companies[j].Stage = stages[v.Companies[j].ID]
			}
		}
		for _, id := range slices.Sorted(maps.Keys(personSince)) {
			if p, here := people[id]; here {
				v.People = append(v.People, TaskPerson{ID: p.ID, Name: displayName(p), Title: p.Title, Company: companyName(deref(p.Maker)),
					Stage: contactStage(p), Since: personSince[id]})
			}
		}
		for j, r := range requestsOf[t.ID] {
			rv := RequestView{TaskRequest: r}
			if c, held := claims[r.ID]; held && r.DoneAt == nil && t.ClosedAt == nil {
				// In the zone the other times on the page come in.
				rv.Holder, rv.HeldBy, rv.HeldUntil = c.Holder, c.Actor, time.UnixMilli(c.ExpiresAt).In(r.AskedAt.Location())
			}
			if j == 0 {
				v.Brief = r.Text
			}
			for _, c := range v.Companies {
				if !c.Since.Before(r.AskedAt) {
					rv.Companies++
				}
			}
			for _, p := range v.People {
				if !p.Since.Before(r.AskedAt) {
					rv.People++
				}
			}
			v.Requests = append(v.Requests, rv)
		}
		for _, n := range notesOf[t.ID] {
			v.Notes = append(v.Notes, NoteView{TaskNote: n, Company: companies[n.CompanyID].Name})
		}
		v.Runs = runsOf(events)
		v.Status = taskStatus(t, v.Open(), v.Last, now)
	}
	return out, nil
}

// TaskCounts is how many of an organisation's tasks are open, and how many it has.
func (s *researchStore) TaskCounts(org int64) (open, all int64, err error) {
	if err := s.InOrg(org).Model(&Task{}).Where("closed_at is null").Count(&open).Error; err != nil {
		return 0, 0, err
	}
	return open, all, s.InOrg(org).Model(&Task{}).Count(&all).Error
}

func (s *researchStore) Task(org, id int64, now time.Time) (*TaskView, error) {
	var t Task
	if err := s.InOrg(org).First(&t, id).Error; err != nil {
		return nil, missing(err, "no task %d in this organisation", id)
	}
	views, err := s.taskViews(s.DB, org, []Task{t}, now)
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// Tasks lists one page of an organisation's tasks, open ones first, newest first.
func (s *researchStore) Tasks(org int64, closed bool, page int, now time.Time) ([]TaskView, Page, error) {
	matching := func() *gorm.DB {
		q := s.InOrg(org).Model(&Task{})
		if !closed {
			q = q.Where("closed_at is null")
		}
		return q
	}
	var total int64
	if err := matching().Count(&total).Error; err != nil {
		return nil, Page{}, err
	}
	pg := pageOf(page, taskPageSize, int(total))
	var tasks []Task
	if err := matching().Order("closed_at is not null, id desc").Limit(pg.Size).Offset(pg.offset()).Find(&tasks).Error; err != nil {
		return nil, Page{}, err
	}
	out, err := s.taskViews(s.DB, org, tasks, now)
	if err != nil {
		return nil, Page{}, err
	}
	if out == nil {
		out = []TaskView{}
	}
	return out, pg, nil
}

// AgentTasks lists a page of the open tasks, newest first. The cursor is the last
// task listed, so one opened meanwhile doesn't shift the pages after it.
func (s *researchStore) AgentTasks(org int64, who Actor, page paging.Page) (briefs []TaskBrief, next string, err error) {
	return s.TaskBriefs(org, who, false, page)
}

// TaskBriefs is AgentTasks with the closed tasks too when asked, after the open ones as
// the tasks page lists them, and only those for one offering when one is given. Its
// cursor then also says whether it ended among the closed.
func (s *researchStore) TaskBriefs(org int64, who Actor, closed bool, page paging.Page) ([]TaskBrief, string, error) {
	return s.TaskBriefsFor(org, who, closed, nil, page)
}

func (s *researchStore) TaskBriefsFor(org int64, who Actor, closed bool, offering *int64, page paging.Page) (briefs []TaskBrief, next string, err error) {
	n := 1
	if closed {
		n = 2
	}
	at, err := page.At(n)
	if err != nil {
		return nil, "", err
	}
	q := s.InOrg(org).Limit(page.Size() + 1)
	if offering != nil {
		q = q.Where("offering_id = ?", *offering)
	}
	switch {
	case !closed:
		q = q.Where("closed_at is null").Order("id desc")
		if at[0] > 0 {
			q = q.Where("id < ?", at[0])
		}
	case at[0] == 1:
		q = q.Where("closed_at is not null and id < ?", at[1]).Order("id desc")
	default:
		q = q.Order("closed_at is not null, id desc")
		if at[1] > 0 {
			q = q.Where("(closed_at is not null or id < ?)", at[1])
		}
	}
	var tasks []Task
	if err := q.Find(&tasks).Error; err != nil {
		return nil, "", err
	}
	if len(tasks) > page.Size() {
		tasks = tasks[:page.Size()]
		last := tasks[len(tasks)-1]
		switch {
		case !closed:
			next = paging.Cursor(last.ID)
		case last.ClosedAt != nil:
			next = paging.Cursor(1, last.ID)
		default:
			next = paging.Cursor(0, last.ID)
		}
	}
	views, err := s.taskViews(s.DB, org, tasks, time.Now())
	if err != nil {
		return nil, "", err
	}
	briefs = []TaskBrief{}
	for _, v := range views {
		briefs = append(briefs, brief(v, who))
	}
	return briefs, next, nil
}

// AgentTask is everything an agent needs to carry a task on. Fetching it is
// recorded, so people see the task was picked up before anything is added, and keeps
// an open request for the agent, so the next one to pick it up takes another.
func (s *researchStore) AgentTask(org int64, who Actor, id int64, page paging.Page) (*TaskOut, error) {
	return s.agentTaskPage(org, who, id, page, time.Now())
}

// agentTask is the task's first page.
func (s *researchStore) agentTask(org int64, who Actor, id int64, now time.Time) (*TaskOut, error) {
	return s.agentTaskPage(org, who, id, paging.Page{}, now)
}

// agentTaskPage answers with a page of each list a task has grown: the companies and
// people found for it, oldest first, and the notes left on it, newest first. The
// cursor holds where each of the three was left. What was asked comes whole.
func (s *researchStore) agentTaskPage(org int64, who Actor, id int64, page paging.Page, now time.Time) (*TaskOut, error) {
	at, err := page.At(3)
	if err != nil {
		return nil, err
	}
	v, err := s.Task(org, id, now)
	if err != nil {
		return nil, err
	}
	var claimed int64
	if v.ClosedAt == nil && !who.ReadOnly {
		if claimed, err = s.claimNext(v, who, now); err != nil {
			return nil, err
		}
		// Read again, so what it returns says who holds what after this claim.
		if v, err = s.Task(org, id, now); err != nil {
			return nil, err
		}
	}
	out := TaskOut{TaskBrief: brief(*v, who), Brief: v.Brief, Requests: []AskedOut{}, Companies: v.Companies, People: v.People, Notes: []NoteOut{},
		Claimed: claimed}
	// A closed task can outlive its offering; an open one can't, as removing it is refused.
	var gone notFound
	if o, err := s.offeringOf(org, v.OfferingID); err != nil && !errors.As(err, &gone) {
		return nil, err
	} else if o != nil {
		out.Offering = &TaskOffering{ID: o.ID, Name: o.Name, HasRoles: o.HasRoles, Guidance: offeringGuidance(o)}
	}
	for _, r := range v.Requests {
		out.Requests = append(out.Requests, asked(r, who))
	}
	// The task's view holds only its latest notes, so a page of them is read here: all
	// of them can then be reached.
	notesFrom := s.InOrg(org).Where("task_id = ?", id).Order("id desc").Limit(page.Size() + 1)
	if at[2] > 0 {
		notesFrom = notesFrom.Where("id < ?", at[2])
	}
	var notes []TaskNote
	if err := notesFrom.Find(&notes).Error; err != nil {
		return nil, err
	}
	notes, at[2] = paging.BeforeID(notes, func(n TaskNote) int64 { return n.ID }, 0, page.Size())
	about := map[int64]string{}
	for _, n := range notes {
		if n.CompanyID != 0 {
			about[n.CompanyID] = ""
		}
	}
	if len(about) > 0 {
		var companies []Company
		if err := s.InOrg(org).Where("id in ?", slices.Collect(maps.Keys(about))).Find(&companies).Error; err != nil {
			return nil, err
		}
		for _, c := range companies {
			about[c.ID] = c.Name
		}
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, NoteOut{ID: n.ID, Kind: n.Kind, Text: n.Text, Actor: n.Actor, At: n.CreatedAt, Company: about[n.CompanyID],
			About: n.CompanyID, ByYou: who.Did(Event{User: n.Actor, KeyID: n.KeyID})})
	}
	// The counts in the brief are of everything found; only the lists are a page.
	out.Companies, at[0] = paging.AfterID(out.Companies, func(c TaskCompany) int64 { return c.ID }, at[0], page.Size())
	out.People, at[1] = paging.AfterID(out.People, func(p TaskPerson) int64 { return p.ID }, at[1], page.Size())
	if at[0] != paging.RanOut || at[1] != paging.RanOut || at[2] != paging.NoneBefore {
		out.NextCursor = paging.Cursor(at...)
	}
	if out.Companies == nil {
		out.Companies = []TaskCompany{}
	}
	if out.People == nil {
		out.People = []TaskPerson{}
	}
	if v.ClosedAt != nil || who.ReadOnly {
		return &out, nil
	}
	// One pickup a run: an agent re-reading the task as it works isn't news. Written
	// apart from the reads above, so two agents on one task don't trip each other.
	var mine Event
	err = s.InOrg(org).Where(`task_id = ? and "user" = ? and key_id = ?`, id, who.Name, who.KeyID).Order("at desc, id desc").First(&mine).Error
	if err == nil && now.Sub(mine.At) < runGap {
		return &out, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	who.Task = id
	return &out, s.LogAs(s.DB, org, 0, nil, who, "task:pickup", nil)
}

func (s *researchStore) AgentNote(org int64, who Actor, in NoteIn) (*TaskNote, error) {
	text, err := words(in.Text, maxNote, "the note")
	if err != nil {
		return nil, err
	}
	if noteKindLabel(in.Kind) == in.Kind {
		return nil, say.Errorf("kind is skipped, dead_end or next, not %q", in.Kind)
	}
	if in.Task == 0 && strings.TrimSpace(in.Company) == "" {
		return nil, say.New("a note is about a task, a company or both")
	}
	if who, err = s.OnTask(org, who, in.Task); err != nil {
		return nil, err
	}
	n := TaskNote{OrgID: org, TaskID: in.Task, Actor: who.Name, KeyID: who.KeyID, Kind: in.Kind, Text: text}
	var company *int64
	if strings.TrimSpace(in.Company) != "" {
		c, err := s.prospecting.companyRef(org, in.Company)
		if err != nil {
			return nil, err
		}
		n.CompanyID, company = c.ID, &c.ID
	}
	return &n, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&n).Error; err != nil {
			return err
		}
		return s.LogAs(tx, org, 0, company, who, "note:"+in.Kind, nil)
	})
}

func (s *researchStore) AgentFinish(org int64, who Actor, in FinishIn) error {
	return s.setRequestDone(org, in.Request, true, who)
}

// claim takes a request for who unless another holds it and it hasn't lapsed; who's
// own claim is renewed. One statement, so of two agents claiming at once only one wins,
// and a request finished since it was read isn't taken.
func (s *researchStore) claim(org, task, request int64, who Actor, now time.Time) (bool, error) {
	res := s.DB.Exec(`insert into request_claims (request_id, org_id, task_id, holder, actor, claimed_at, expires_at)
		select ?, ?, ?, ?, ?, ?, ?
		where exists (select 1 from task_requests where id = ? and org_id = ? and task_id = ? and done_at is null)
		on conflict (request_id) do update set holder = excluded.holder, actor = excluded.actor, expires_at = excluded.expires_at,
			claimed_at = case when request_claims.holder = excluded.holder then request_claims.claimed_at else excluded.claimed_at end
		where request_claims.expires_at <= ? or request_claims.holder = excluded.holder`,
		request, org, task, holderOf(who), who.Name, now.UnixMilli(), now.Add(claimFor).UnixMilli(),
		request, org, task, now.UnixMilli())
	return res.RowsAffected == 1, res.Error
}

// renewClaims is the heartbeat an agent can't forget: each write it makes for a task
// keeps the requests it holds there.
func (s *researchStore) renewClaims(tx *gorm.DB, org, task int64, who Actor, now time.Time) error {
	return tx.Model(&RequestClaim{}).Where("org_id = ? and task_id = ? and holder = ? and expires_at > ?", org, task, holderOf(who), now.UnixMilli()).
		Update("expires_at", now.Add(claimFor).UnixMilli()).Error
}

// claimNext keeps an open request for who: one it already holds, or else the oldest
// nobody else does. It returns 0 when other agents hold every open request.
func (s *researchStore) claimNext(v *TaskView, who Actor, now time.Time) (int64, error) {
	open := v.Open()
	for _, r := range open {
		if r.Holder != holderOf(who) {
			continue
		}
		// Its claim may have lapsed and gone to another since the task was read.
		if won, err := s.claim(v.OrgID, v.ID, r.ID, who, now); err != nil || won {
			return r.ID, err
		}
	}
	for _, r := range open {
		if r.Holder != "" {
			continue
		}
		// Someone else may have taken it since the task was read; then try the next.
		won, err := s.claim(v.OrgID, v.ID, r.ID, who, now)
		if err != nil || won {
			return r.ID, err
		}
	}
	return 0, nil
}

func (s *researchStore) Offerings(org int64) ([]Offering, error) {
	var out []Offering
	return out, s.InOrg(org).Order("lower(name), id").Find(&out).Error
}

func (s *researchStore) Offering(org, id int64) (*Offering, error) {
	var o Offering
	if err := s.InOrg(org).Where("id = ?", id).First(&o).Error; err != nil {
		return nil, missing(err, "no offering %d", id)
	}
	return &o, nil
}

// offeringOf is the offering with this id, or nil for the organisation's own.
func (s *researchStore) offeringOf(org, id int64) (*Offering, error) {
	if id == 0 {
		return nil, nil
	}
	return s.Offering(org, id)
}

func (s *researchStore) AddOffering(org int64, o Offering, actor string) (*Offering, error) {
	if err := o.Check(); err != nil {
		return nil, err
	}
	o.ID, o.OrgID, o.UpdatedBy = 0, org, actor
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		if err := nameFree(tx, org, 0, o.Name); err != nil {
			return err
		}
		if err := tx.Create(&o).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, "offering:added", &o.Name)
	})
	return &o, err
}

// ChangeOffering applies change to the offering as it is now, and saves it.
func (s *researchStore) ChangeOffering(org, id int64, change func(*Offering), actor string) (*Offering, error) {
	var o *Offering
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		var err error
		if o, err = heldOffering(tx, org, id); err != nil {
			return err
		}
		had := o.HasRoles
		change(o)
		if err := o.Check(); err != nil {
			return err
		}
		if had && !o.HasRoles {
			var n int64
			if err := tx.Model(&Role{}).Where("org_id = ? and offering_id = ?", org, o.ID).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return conflict{say.Errorf("%s has %d roles, so it keeps them", o.Name, n)}
			}
		}
		if err := nameFree(tx, org, o.ID, o.Name); err != nil {
			return err
		}
		o.UpdatedBy = actor
		if err := tx.Model(o).Select("name", "look_for", "pitch", "note_template", "has_roles", "updated_by", "updated_at").Updates(o).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, "offering:changed", &o.Name)
	})
	return o, err
}

// RemoveOffering deletes an offering with every verdict and role kept for it; the history keeps
// what was decided. People being reached for it are then reached for the organisation's
// own, and companies found only for it come up in the organisation's own review.
func (s *researchStore) RemoveOffering(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Companies are held before the offering, as everywhere; holding the organisation
		// keeps any more from being found for it meanwhile.
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		if err := tx.Model(&Company{}).Where("org_id = ? and found_for = ?", org, id).Update("found_for", 0).Error; err != nil {
			return err
		}
		o, err := heldOffering(tx, org, id)
		if err != nil {
			return err
		}
		var open int64
		if err := tx.Model(&Task{}).Where("org_id = ? and offering_id = ? and closed_at is null", org, id).Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return conflict{say.Errorf("%d open tasks are finding companies for %s: close them first", open, o.Name)}
		}
		if err := tx.Where("org_id = ? and offering_id = ?", org, id).Delete(&Verdict{}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and offering_id = ?", org, id).Delete(&Role{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Contact{}).Where("org_id = ? and offering_id = ?", org, id).Update("offering_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Delete(o).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, "offering:removed", &o.Name)
	})
}

// JudgeCompany says where a company stands for an offering; offering 0 is the
// organisation's own, whose verdict is the company's stage.
func (s *researchStore) JudgeCompany(org, offering, company int64, stage, fit, reason, actor string) error {
	if offering == 0 {
		if strings.TrimSpace(reason) != "" {
			return say.New("a reason is kept with a verdict for an offering; for the organisation's own, say why in fit")
		}
		return s.prospecting.SetCompanyStage(org, company, stage, fit, actor)
	}
	if stage != CompanyQualified && stage != CompanyRejected && stage != CompanyReview {
		return say.Errorf("unknown stage %q", stage)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		o, err := heldOffering(tx, org, offering)
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Company{}).Where("org_id = ? and id = ?", org, company).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return noSuch("no company %d", company)
		}
		if byRoles, err := judgedByRoles(tx, org, o, company); err != nil || byRoles {
			if byRoles {
				return conflict{say.Errorf("this company has roles for %s, and where it stands follows them: pursue or junk its roles", o.Name)}
			}
			return err
		}
		v := Verdict{OrgID: org, OfferingID: offering, CompanyID: company, Stage: stage, Fit: fit,
			Reason: strings.TrimSpace(reason), By: actor, UpdatedAt: time.Now()}
		update := []string{"stage", "reason", "decided_by", "updated_at"}
		if fit != "" {
			update = append(update, "fit")
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "org_id"}, {Name: "offering_id"}, {Name: "company_id"}},
			DoUpdates: clause.AssignmentColumns(update),
		}).Create(&v).Error; err != nil {
			return err
		}
		said := o.Name
		if v.Reason != "" {
			said += ": " + v.Reason
		}
		return s.Log(tx, org, 0, &company, actor, "verdict:"+stage, &said)
	})
}

// ToReview is how many companies wait for review, counting each offering's queue.
func (s *researchStore) ToReview(org int64) int {
	var n int64
	_ = logged(s.InOrg(org).Model(&Verdict{}).Where("stage = ?", CompanyReview).Count(&n))
	return s.prospecting.ResearchCount(org, "review") + int(n)
}

// WithVerdicts shows Research rows as they stand for an offering: its stage, and its
// grade where a person gave one.
func (s *researchStore) WithVerdicts(org, offering int64, rows []ResearchRow) error {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var verdicts []Verdict
	if err := s.InOrg(org).Where("offering_id = ? and company_id in ?", offering, ids).Find(&verdicts).Error; err != nil {
		return err
	}
	by := map[int64]Verdict{}
	for _, v := range verdicts {
		by[v.CompanyID] = v
	}
	for i := range rows {
		v, ok := by[rows[i].ID]
		rows[i].Stage = CompanyReview
		if ok {
			rows[i].Stage = v.Stage
		}
		if v.Fit != "" {
			rows[i].Fit = v.Fit
		}
	}
	return nil
}

func (s *researchStore) standing(org, company int64) ([]OfferingStanding, error) {
	var out []OfferingStanding
	return out, s.DB.Model(&Verdict{}).Select("offerings.name as offering, verdicts.stage, verdicts.reason").
		Joins("join offerings on offerings.id = verdicts.offering_id and offerings.org_id = verdicts.org_id").
		Where("verdicts.org_id = ? and verdicts.company_id = ?", org, company).Order("lower(offerings.name)").Scan(&out).Error
}

func (s *researchStore) verdictRows(org int64) *gorm.DB {
	return s.DB.Model(&Verdict{}).Select("verdicts.*, companies.name as company").
		Joins("join companies on companies.id = verdicts.company_id and companies.org_id = verdicts.org_id").
		Joins("join offerings on offerings.id = verdicts.offering_id and offerings.org_id = verdicts.org_id").
		Where("verdicts.org_id = ?", org)
}

// Verdicts are where a company stands for each offering it has been judged for.
func (s *researchStore) Verdicts(org, company int64) ([]VerdictRow, error) {
	var n int64
	if err := s.InOrg(org).Model(&Company{}).Where("id = ?", company).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, noSuch("no company %d", company)
	}
	var out []VerdictRow
	return out, s.verdictRows(org).Where("verdicts.company_id = ?", company).Order("verdicts.offering_id").Scan(&out).Error
}

// Judged lists an offering's verdicts at one stage, or at every stage when stage is
// empty, latest first; a company ruled out by mistake is found here and put back.
func (s *researchStore) Judged(org, offering int64, stage string) ([]VerdictRow, error) {
	if _, err := s.Offering(org, offering); err != nil {
		return nil, err
	}
	q := s.verdictRows(org).Where("verdicts.offering_id = ?", offering)
	if stage != "" {
		q = q.Where("verdicts.stage = ?", stage)
	}
	var out []VerdictRow
	return out, q.Order("verdicts.updated_at desc, verdicts.id desc").Scan(&out).Error
}

// addRole adds a role to a company held by the caller, for an offering it has also held.
// A new role brings a company back to review for the offering, unless a person ruled it
// out themselves rather than by junking its roles.
func (s *researchStore) addRole(tx *gorm.DB, org int64, o *Offering, company int64, in RoleIn, who Actor) (*Role, bool, error) {
	if had, err := sameRole(tx, org, o.ID, company, in); err != nil || had != nil {
		if had != nil && had.Stage == RoleJunked {
			return had, true, conflict{junkedSaid(*had)}
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
		if v.Reason != "" {
			return nil, false, conflict{say.Errorf("%s rejected this company for %s: %s. Don't add its roles", v.By, o.Name, v.Reason)}
		}
		return nil, false, conflict{say.Errorf("%s rejected this company for %s. Don't add its roles", v.By, o.Name)}
	}
	r := Role{OrgID: org, OfferingID: o.ID, CompanyID: company, Title: in.Title, URL: in.SourceURL, Details: in.Details,
		Stage: RoleReview, FoundBy: who.Name, TaskID: who.Task}
	if err := tx.Create(&r).Error; err != nil {
		return nil, false, err
	}
	if err := s.LogAs(tx, org, 0, &company, who, "add:role", str(o.Name+": "+r.Title)); err != nil {
		return nil, false, err
	}
	return &r, false, s.followRoles(tx, org, o, company, who.Name)
}

// followRoles sets the company's verdict for the offering from its roles: up for review
// while one waits, so it is in a queue someone works, else qualified while one is
// pursued, and ruled out once every one is junked.
func (s *researchStore) followRoles(tx *gorm.DB, org int64, o *Offering, company int64, actor string) error {
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
	return s.Log(tx, org, 0, &company, actor, "verdict:"+want, &said)
}

// AddRole is a person adding a role they found to a company, for an offering with roles.
func (s *researchStore) AddRole(org, offering, company int64, in RoleIn, actor string) (*Role, error) {
	if err := in.check(false); err != nil {
		return nil, err
	}
	var r *Role
	err := s.DB.Transaction(func(tx *gorm.DB) error {
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
			return conflict{say.Errorf("%s already has this role, as role %d", o.Name, r.ID)}
		}
		return nil
	})
	return r, err
}

func (s *researchStore) companyIn(tx *gorm.DB, org, id int64) (*Company, error) {
	var c Company
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
		return nil, missing(err, "no company %d", id)
	}
	return &c, nil
}

// JudgeRole pursues or junks a role, or puts it back up for review, and brings the
// company's verdict for its offering into line.
func (s *researchStore) JudgeRole(org, id int64, stage, reason, actor string) (*Role, error) {
	if stage != RoleReview && stage != RolePursued && stage != RoleJunked {
		return nil, say.Errorf("unknown stage %q: review, pursued or junked", stage)
	}
	reason = strings.TrimSpace(reason)
	var r Role
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&r).Error; err != nil {
			return missing(err, "no role %d", id)
		}
		if stage != RoleJunked && reason != "" {
			return say.New("a reason is kept only for a junked role")
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
		if err := s.Log(tx, org, 0, &r.CompanyID, actor, "role:"+stage, &said); err != nil {
			return err
		}
		return s.followRoles(tx, org, o, r.CompanyID, actor)
	})
	return &r, err
}

// Roles are a company's roles for every offering, or for one when offering is set.
func (s *researchStore) Roles(org, company int64, offering *int64) ([]Role, error) {
	if _, err := s.companyIn(s.DB, org, company); err != nil {
		return nil, err
	}
	q := s.InOrg(org).Where("company_id = ?", company)
	if offering != nil {
		q = q.Where("offering_id = ?", *offering)
	}
	var out []Role
	return out, q.Order("offering_id, id").Find(&out).Error
}

// RolesAt are the roles for an offering at each of these companies.
func (s *researchStore) RolesAt(org, offering int64, companies []int64) (map[int64][]Role, error) {
	out := map[int64][]Role{}
	if offering == 0 || len(companies) == 0 {
		return out, nil
	}
	var rows []Role
	if err := s.InOrg(org).Where("offering_id = ? and company_id in ?", offering, companies).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CompanyID] = append(out[r.CompanyID], r)
	}
	return out, nil
}

// AgentAddRole adds a role an agent found for its task's offering. One junked before is
// refused with why, so it isn't brought back.
func (s *researchStore) AgentAddRole(org int64, who Actor, in AddRoleIn) (*AddRoleOut, error) {
	who, err := s.OnTask(org, who, in.Task)
	if err != nil {
		return nil, err
	}
	if who.Offering == 0 {
		return nil, say.New("add_role is for a task finding roles for an offering that has them; this isn't one, so add the role as an open_roles fact")
	}
	if err := in.check(true); err != nil {
		return nil, err
	}
	c, err := s.prospecting.companyRef(org, in.Company)
	if err != nil {
		return nil, err
	}
	var out AddRoleOut
	err = s.DB.Transaction(func(tx *gorm.DB) error {
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
		if err := s.prospecting.heldWhenFound(tx, org, who, c, stages[c.ID]); err != nil {
			return err
		}
		return s.matched(tx, org, who, 0, &c.ID)
	})
	return &out, err
}

func (s *researchStore) roleBriefs(org, company int64) ([]RoleBrief, error) {
	var out []RoleBrief
	return out, s.DB.Model(&Role{}).Select("roles.id, offerings.name as offering, roles.title, roles.url, roles.details, roles.stage, roles.reason").
		Joins("join offerings on offerings.id = roles.offering_id and offerings.org_id = roles.org_id").
		Where("roles.org_id = ? and roles.company_id = ?", org, company).Order("lower(offerings.name), roles.id").Scan(&out).Error
}

func (s *researchStore) AgentAddCompany(org int64, who Actor, in AddCompanyIn) (*AddCompanyOut, error) {
	who, err := s.OnTask(org, who, in.Task)
	if err != nil {
		return nil, err
	}
	if in.ConfiguratorURL != "" && !httpURL.MatchString(strings.TrimSpace(in.ConfiguratorURL)) {
		return nil, say.New("configurator_url must be an http(s) link")
	}
	for _, f := range in.Facts {
		if err := checkFact(f); err != nil {
			return nil, err
		}
	}
	var out AddCompanyOut
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := holdOrg(tx, org); err != nil {
			return err
		}
		domain := bareDomain(in.Domain)
		var c *Company
		var named int64
		err := tx.Model(&Company{}).Where("org_id = ? and lower(name) = ?", org, strings.ToLower(strings.TrimSpace(in.Name))).Count(&named).Error
		if err == nil && named == 0 {
			c, err = companyAt(tx, org, domain)
		}
		if err != nil {
			return err
		}
		created := false
		if c == nil {
			c, created, err = ensureCompany(tx, org, Company{Name: in.Name, Domain: domain, Country: in.Country,
				Segment: in.Segment, Fit: in.Fit, Why: in.Why, FoundBy: who.Name, Stage: CompanyReview, FoundFor: who.Offering})
			if err != nil {
				return err
			}
		}
		out = AddCompanyOut{ID: c.ID, Name: c.Name, Existed: !created, Stage: c.Stage}
		if err := hold(tx, &Company{}, org, c.ID); err != nil {
			return err
		}
		if who.Offering != 0 {
			if out.Stage, err = putUpForReview(tx, org, who.Offering, c.ID, who.Name); err != nil {
				return err
			}
		} else if err := foundForOwn(tx, org, c.ID); err != nil {
			return err
		}
		if !created {
			if err := s.prospecting.heldWhenFound(tx, org, who, c, out.Stage); err != nil {
				return err
			}
		}
		added := 0
		for _, f := range in.Facts {
			_, had, err := addFact(tx, org, c.ID, f, who.Name)
			if err != nil {
				return err
			}
			if !had {
				added++
			}
		}
		// Older clients' fields become facts; with no configurator link they cite nothing, as they did as columns.
		older, err := addConfiguratorFacts(tx, org, c.ID, in.ConfiguratorURL, in.Platform, in.BuiltBy, who.Name)
		if err != nil {
			return err
		}
		added += older
		if !created && added == 0 {
			return s.matched(tx, org, who, 0, &c.ID)
		}
		action := "add:company"
		if !created {
			action = "add:facts"
		}
		return s.LogAs(tx, org, 0, &c.ID, who, action, str(fmt.Sprintf("%s, %d facts", c.Name, added)))
	})
	return &out, err
}

func (s *researchStore) AgentAddFact(org int64, who Actor, in AddFactIn) (*AddFactOut, error) {
	who, err := s.OnTask(org, who, in.Task)
	if err != nil {
		return nil, err
	}
	if err := checkFact(in.FactIn); err != nil {
		return nil, err
	}
	c, err := s.prospecting.companyRef(org, in.Company)
	if err != nil {
		return nil, err
	}
	var out AddFactOut
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := hold(tx, &Company{}, org, c.ID); err != nil {
			return err
		}
		if err := forOffering(tx, org, who, c.ID); err != nil {
			return err
		}
		if err := s.prospecting.heldWhenReached(tx, org, who, c); err != nil {
			return err
		}
		f, had, err := addFact(tx, org, c.ID, in.FactIn, who.Name)
		if err != nil {
			return err
		}
		out = AddFactOut{Fact: *f, Existed: had}
		if had {
			return s.matched(tx, org, who, 0, &c.ID)
		}
		return s.LogAs(tx, org, 0, &c.ID, who, "add:fact", str(f.Field))
	})
	return &out, err
}

func (s *researchStore) AgentAddContact(o *Org, who Actor, in AddContactIn) (*AddContactOut, error) {
	who, err := s.OnTask(o.ID, who, in.Task)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, say.New("name is required")
	}
	if !httpURL.MatchString(strings.TrimSpace(in.SourceURL)) {
		return nil, say.New("source_url is required and must start with http")
	}
	if in.EmailStatus != "" && in.EmailStatus != "verified" && in.EmailStatus != "guess" {
		return nil, say.New("email_status is verified, guess or empty")
	}
	if in.Linkedin != "" && !httpURL.MatchString(strings.TrimSpace(in.Linkedin)) {
		return nil, say.New("linkedin must be an https link")
	}
	if err := s.prospecting.Allowed(o, in.Email, in.Linkedin); err != nil {
		s.prospecting.withheld(o.ID, who, withheldNeverContact)
		return nil, err
	}
	if err := s.outreach.checkMailDomains(in.Email); err != nil {
		return nil, err
	}
	c, err := s.prospecting.companyRef(o.ID, in.Company)
	if err != nil {
		return nil, err
	}
	if err := erased(s.DB, o.ID, in.Name, c.Name, in.Email, in.Linkedin); err != nil {
		if errors.Is(err, errErased) {
			s.prospecting.withheld(o.ID, who, withheldErased)
		}
		return nil, err
	}
	person := strings.TrimSpace(in.Name)
	if in.Title != "" {
		person += ", " + strings.TrimSpace(in.Title)
	}
	first := firstField(in.Name)
	var out AddContactOut
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// The organisation too: a contact's key is one of a kind across it.
		if err := holdOrg(tx, o.ID); err != nil {
			return err
		}
		if err := hold(tx, &Company{}, o.ID, c.ID); err != nil {
			return err
		}
		if err := forOffering(tx, o.ID, who, c.ID); err != nil {
			return err
		}
		if err := s.prospecting.heldWhenReached(tx, o.ID, who, c); err != nil {
			return err
		}
		var people []Contact
		if err := tx.Where("org_id = ? and company_id = ? and key not like 'org:%'", o.ID, c.ID).Order("id").Find(&people).Error; err != nil {
			return err
		}
		if p := samePerson(people, in); p != nil {
			waiting, err := s.prospecting.suggestDetails(tx, who, *p, in)
			if err != nil {
				return err
			}
			out = AddContactOut{ID: p.ID, Status: p.Status, Existed: true, Suggested: waiting}
			return s.matched(tx, o.ID, who, p.ID, &c.ID)
		}
		contact := Contact{OrgID: o.ID, Key: uniqueKey(tx, o.ID, personKey(in.Name)), File: "added by " + who.Name,
			Person: &person, First: &first, Maker: &c.Name, CompanyID: &c.ID, Title: strings.TrimSpace(in.Title),
			Email: nonEmpty(strings.TrimSpace(in.Email)), EmailStatus: in.EmailStatus, Linkedin: nonEmpty(strings.TrimSpace(in.Linkedin)),
			Phone: nonEmpty(strings.TrimSpace(in.Phone)), Sources: str(strings.TrimSpace(in.SourceURL)), Status: ContactSuggested, OfferingID: who.Offering}
		contact.Tz = tz.Guess(in.Phone, "("+c.Country+")")
		if err := tx.Create(&contact).Error; err != nil {
			return err
		}
		out = AddContactOut{ID: contact.ID, Status: contact.Status}
		return s.LogAs(tx, o.ID, contact.ID, &c.ID, who, "add:contact", nil)
	})
	return &out, err
}

func (s *researchStore) AgentDraft(org int64, who Actor, in DraftIn) error {
	who, err := s.OnTask(org, who, in.Task)
	if err != nil {
		return err
	}
	field, ok := draftFields[in.Kind]
	if !ok {
		return say.Errorf("kind is note, message, email or followup, not %q", in.Kind)
	}
	if strings.TrimSpace(in.Text) == "" {
		return say.New("text is empty")
	}
	if in.Kind == "note" && len([]rune(in.Text)) > 300 {
		return say.New("LinkedIn notes are 300 characters at most")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// A person's edit made meanwhile is then either seen below or lands on top of this.
		if err := hold(tx, &Contact{}, org, in.ContactID); err != nil {
			return err
		}
		var c Contact
		if err := tx.Where("org_id = ? and id = ?", org, in.ContactID).First(&c).Error; err != nil {
			return missing(err, "no contact %d", in.ContactID)
		}
		if day := draftSent(c, in.Kind); day != "" {
			return say.Errorf("that went out on %s, so its text stays as sent", day)
		}
		current := map[string]*string{"note": c.Note, "message": c.Message, "body": c.Body, "followup": c.Followup}[field]
		if deref(current) != "" {
			person, err := editedBy(tx, org, c.ID, field, in.Kind)
			if err != nil {
				return err
			}
			if person != "" {
				return say.Errorf("%s edited this text, so it is theirs to change", person)
			}
		}
		updates := map[string]any{field: strings.TrimSpace(in.Text), "draft_by": who.Name}
		if in.Kind == "email" && in.Subject != "" {
			updates["subject"] = in.Subject
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		return s.LogAs(tx, org, c.ID, c.CompanyID, who, "draft:"+in.Kind, nil)
	})
}

func (s *researchStore) lastAgents(org int64, who Actor, companies []int64) (map[int64]*LastAgent, error) {
	out := map[int64]*LastAgent{}
	if len(companies) == 0 {
		return out, nil
	}
	latest := s.DB.Model(&Event{}).Select("max(id)").
		Where(`org_id = ? and company_id in ? and "user" like 'agent:%'`, org, companies).Group("company_id")
	var events []Event
	if err := s.InOrg(org).Where("id in (?)", latest).Find(&events).Error; err != nil {
		return nil, err
	}
	for _, e := range events {
		out[*e.CompanyID] = &LastAgent{Actor: e.User, At: e.At, ByYou: who.Did(e)}
	}
	return out, nil
}

// AgentSearch lists a page of matching companies and a page of matching people, each
// in the order they were added. The cursor holds the last of each kind listed.
func (s *researchStore) AgentSearch(org int64, who Actor, q string, page paging.Page) (hits []SearchHit, next string, err error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) < 2 {
		return nil, "", say.New("search for at least two characters")
	}
	at, err := page.At(2)
	if err != nil {
		return nil, "", err
	}
	size, more := page.Size(), false
	var companies []Company
	cond, args := s.prospecting.companyMatch(q)
	if err := s.InOrg(org).Where("id > ?", at[0]).Where(cond, args...).Order("id").Limit(size + 1).Find(&companies).Error; err != nil {
		return nil, "", err
	}
	if at[0] = paging.RanOut; len(companies) > size {
		companies, more = companies[:size], true
		at[0] = companies[size-1].ID
	}
	ids := make([]int64, len(companies))
	for i, c := range companies {
		ids[i] = c.ID
	}
	last, err := s.lastAgents(org, who, ids)
	if err != nil {
		return nil, "", err
	}
	hits = []SearchHit{}
	for _, c := range companies {
		hits = append(hits, SearchHit{Kind: "company", ID: c.ID, Name: c.Name, Stage: c.Stage, LastAgent: last[c.ID]})
	}
	var people []Contact
	cond, args = s.prospecting.contactMatch(q, true)
	if err := s.InOrg(org).Where("id > ? and key not like 'org:%'", at[1]).Where(cond, args...).Order("id").Limit(size + 1).Find(&people).Error; err != nil {
		return nil, "", err
	}
	if at[1] = paging.RanOut; len(people) > size {
		people, more = people[:size], true
		at[1] = people[size-1].ID
	}
	for _, p := range people {
		hits = append(hits, SearchHit{Kind: "contact", ID: p.ID, Name: displayName(p), Company: deref(p.Maker), Stage: contactStage(p)})
	}
	if more {
		next = paging.Cursor(at...)
	}
	return hits, next, nil
}

// AgentCompany is a company with a page of each list it has: its facts and people,
// oldest first, and its history and notes, newest first. The cursor holds where each
// of the four was left. With no limit asked for, the history and the notes come in
// their own shorter pages: the latest is what an agent starting on a company needs.
func (s *researchStore) AgentCompany(org int64, who Actor, ref string, page paging.Page) (*CompanyDetail, error) {
	at, err := page.At(4)
	if err != nil {
		return nil, err
	}
	c, err := s.prospecting.companyRef(org, ref)
	if err != nil {
		return nil, err
	}
	facts, err := s.prospecting.Facts(org, c.ID)
	if err != nil {
		return nil, err
	}
	people, err := s.prospecting.People(org, c.ID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(people, func(a, b Contact) int { return cmp.Compare(a.ID, b.ID) })
	names := map[int64]string{}
	for _, p := range people {
		names[p.ID] = displayName(p)
	}
	size, historySize, notesSize := page.Size(), page.Size(), page.Size()
	// Only the first answer is the short one: carrying on from it reads full pages, so
	// the rest takes a few calls and not one for every three notes.
	if page.Limit <= 0 && page.Cursor == "" {
		historySize, notesSize = agentHistory, agentNotes
	}
	out := &CompanyDetail{Company: *c, People: []PersonBrief{}, History: []Happened{}, Notes: []NoteOut{}}
	if out.Standing, err = s.standing(org, c.ID); err != nil {
		return nil, err
	}
	if out.Roles, err = s.roleBriefs(org, c.ID); err != nil {
		return nil, err
	}
	out.Facts, at[0] = paging.AfterID(facts, func(f Fact) int64 { return f.ID }, at[0], size)
	if out.Facts == nil {
		out.Facts = []Fact{}
	}
	people, at[1] = paging.AfterID(people, func(p Contact) int64 { return p.ID }, at[1], size)
	for _, p := range people {
		out.People = append(out.People, PersonBrief{ID: p.ID, Name: displayName(p), Title: p.Title, Email: deref(p.Email),
			EmailStatus: p.EmailStatus, Linkedin: deref(p.Linkedin), Stage: contactStage(p),
			NextStep: p.NextStep, NextDate: deref(p.NextDate), LostReason: p.LostReason})
	}
	last, err := s.lastAgents(org, who, []int64{c.ID})
	if err != nil {
		return nil, err
	}
	out.LastAgent = last[c.ID]

	// Newest first, and only as many as a page and one more are read.
	before := func(id int64) *gorm.DB {
		q := s.InOrg(org).Where("company_id = ?", c.ID).Order("id desc")
		if id > 0 {
			q = q.Where("id < ?", id)
		}
		return q
	}
	var events []Event
	if err := before(at[2]).Limit(historySize + 1).Find(&events).Error; err != nil {
		return nil, err
	}
	events, at[2] = paging.BeforeID(events, func(e Event) int64 { return e.ID }, 0, historySize)
	for _, e := range events {
		// Only what erasure would keep: an event's value can hold a note or someone's details.
		out.History = append(out.History, Happened{At: e.At, Actor: e.User, Action: e.Action, Value: deref(keptValue(e)),
			Person: names[e.ContactID], ByYou: who.Did(e)})
	}
	var notes []TaskNote
	if err := before(at[3]).Limit(notesSize + 1).Find(&notes).Error; err != nil {
		return nil, err
	}
	notes, at[3] = paging.BeforeID(notes, func(n TaskNote) int64 { return n.ID }, 0, notesSize)
	for _, n := range notes {
		out.Notes = append(out.Notes, NoteOut{ID: n.ID, Kind: n.Kind, Text: n.Text, Actor: n.Actor, At: n.CreatedAt,
			ByYou: who.Did(Event{User: n.Actor, KeyID: n.KeyID})})
	}
	if at[0] != paging.RanOut || at[1] != paging.RanOut || at[2] != paging.NoneBefore || at[3] != paging.NoneBefore {
		out.NextCursor = paging.Cursor(at...)
	}
	return out, nil
}

// AgentDue lists a page of what is due, step by step in the order Today works them.
// The queues are worked out whole each time, so the cursor is how far down them the
// last page ended: someone marked in between moves the rest up by one.
func (s *researchStore) AgentDue(o *Org, page paging.Page) (*DueOut, error) {
	at, err := page.At(1)
	if err != nil {
		return nil, err
	}
	items := []DueItem{}
	for _, step := range []string{"linkedin", "accepted", "email", "followup", "call"} {
		rows, err := s.prospecting.Due(o.ID, step, time.Now(), o.Settings)
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			text := map[string]string{"linkedin": deref(c.Note), "accepted": deref(c.Message), "email": deref(c.Body), "followup": deref(c.Followup), "call": "x"}[step]
			items = append(items, DueItem{Step: step, ContactID: c.ID, Name: displayName(c), Company: deref(c.Maker), HasDraft: text != ""})
		}
	}
	from := min(int(at[0]), len(items))
	to := min(from+page.Size(), len(items))
	out := &DueOut{Items: items[from:to]}
	if to < len(items) {
		out.NextCursor = paging.Cursor(int64(to))
	}
	return out, nil
}

// AgentScores is each agent's record: how many of its companies people qualified
// or rejected, and how many of its contacts were picked and later replied.
func (s *researchStore) AgentScores(org int64) []AgentScore {
	by := map[string]*AgentScore{}
	get := func(actor string) *AgentScore {
		name := strings.TrimPrefix(actor, "agent:")
		if by[name] == nil {
			by[name] = &AgentScore{Agent: name}
		}
		return by[name]
	}
	var companies []struct {
		FoundBy, Stage string
		N              int
	}
	_ = logged(s.DB.Raw(`select found_by, stage, count(*) as n from companies
		where org_id = ? and found_by like 'agent:%' group by found_by, stage`, org).Scan(&companies))
	for _, r := range companies {
		a := get(r.FoundBy)
		a.Companies += r.N
		switch r.Stage {
		case CompanyQualified:
			a.Qualified += r.N
		case CompanyRejected:
			a.Rejected += r.N
		}
	}
	var contacts []struct {
		Actor                     string
		Contacts, Picked, Replied int
	}
	_ = logged(s.DB.Raw(`select e."user" as actor, count(*) as contacts,
		sum(case when exists (select 1 from events p where p.org_id = e.org_id and p.contact_id = e.contact_id and p.action = 'pick') then 1 else 0 end) as picked,
		sum(case when c.replied is not null then 1 else 0 end) as replied
		from events e join contacts c on c.id = e.contact_id
		where e.org_id = ? and e.action = 'add:contact' and e."user" like 'agent:%' group by e."user"`, org).Scan(&contacts))
	for _, r := range contacts {
		a := get(r.Actor)
		a.Contacts, a.Picked, a.Replied = r.Contacts, r.Picked, r.Replied
	}
	out := make([]AgentScore, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// AgentMark records an outcome for a contact already in outreach. Suggested
// contacts haven't been picked by a person yet, so nothing has happened to them.
func (s *researchStore) AgentMark(org int64, who Actor, in MarkIn) error {
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today()
	}
	return s.prospecting.mark(org, in.ContactID, in.Step, date, who, strings.TrimSpace(in.Note))
}

// AgentMarks is, per contact and step, the agent behind the latest mark, so a
// person re-marking a step takes it back.
func (s *researchStore) AgentMarks(org int64, ids []int64) map[int64]map[string]string {
	out := map[int64]map[string]string{}
	if len(ids) == 0 {
		return out
	}
	var events []Event
	_ = logged(s.InOrg(org).Where("contact_id in ? and action like 'mark:%'", ids).Order("at, id").Find(&events))
	for _, e := range events {
		step := strings.TrimPrefix(e.Action, "mark:")
		if out[e.ContactID] == nil {
			out[e.ContactID] = map[string]string{}
		}
		if strings.HasPrefix(e.User, "agent:") {
			out[e.ContactID][step] = e.User
		} else {
			delete(out[e.ContactID], step)
		}
	}
	return out
}
