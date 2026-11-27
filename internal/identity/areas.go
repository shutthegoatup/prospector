package identity

const AreaSales = "sales"

const AreaSupport = "support"

// Areas split a team's work. Everyone reads everything; changing something takes
// its area, and an owner has every area.
var Areas = []struct{ Name, What string }{
	{AreaSales, "research, outreach, deals and tasks for agents"},
	{AreaSupport, "enquiries and their replies, subscriptions and onboarding"},
}
