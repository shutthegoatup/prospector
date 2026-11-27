package store

// Later points at laterColumns' fields, in that order.
func (c *Contact) Later() []any {
	return []any{&c.Status, &c.Title, &c.EmailStatus, &c.Stage, &c.NextStep, &c.NextDate, &c.LostReason, &c.DraftBy, &c.OfferingID}
}

func (c *Contact) Values() []*string {
	return []*string{nil, &c.Key, &c.File, c.Section, c.Fit, c.Maker, c.Person, c.First, c.Linkedin, c.Email,
		c.Note, c.Subject, c.Body, c.Followup, c.Flags, c.LinkedinAdded, c.EmailSent, c.FollowupSent,
		c.Replied, c.Declined, c.Hold, c.Sources, c.Phone, c.Called, c.CallNotes, c.Tz, c.SendTo, c.Accepted, c.Message}
}

func (c *Contact) Fields() []**string {
	return []**string{nil, nil, nil, &c.Section, &c.Fit, &c.Maker, &c.Person, &c.First, &c.Linkedin, &c.Email,
		&c.Note, &c.Subject, &c.Body, &c.Followup, &c.Flags, &c.LinkedinAdded, &c.EmailSent, &c.FollowupSent,
		&c.Replied, &c.Declined, &c.Hold, &c.Sources, &c.Phone, &c.Called, &c.CallNotes, &c.Tz, &c.SendTo, &c.Accepted, &c.Message}
}
