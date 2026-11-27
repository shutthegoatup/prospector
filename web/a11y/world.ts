// Who and what the accessibility run's server holds: invented, on .example domains.
export const port = Number(process.env.A11Y_PORT ?? 8791);
export const base = `http://127.0.0.1:${port}`;
export const member = { email: "robin@prospector.example", name: "Robin Ash", password: "a11y-correct-horse-battery" };
export const org = { slug: "alpha", name: "Alpha Widgets" };
// A member who does neither area's work, so every page shows them what only reading is.
export const reader = { email: "sky@prospector.example", name: "Sky Reed", password: "a11y-reader-battery-staple" };

// What setup.ts fills the organisation with. Pages show it as it is, so the en-XA check
// takes it for data, not words of the app's.
export const seed = {
  people: [
    { name: "Ada Brook", title: "Head of Operations", company: "Brook & Vale", email: "ada@brookvale.example" },
    { name: "Kit Moreau", title: "Founder", company: "Moreau Freight", email: "kit@moreau.example", added: true },
    { name: "Sol Nakamura", title: "Buyer", company: "Northwind Parts", email: "sol@northwind.example" },
  ],
  campaign: { name: "Spring update", subject: "What's new", body: "Hello,\n\nA short note." },
  task: { title: "Find freight firms", brief: "Freight firms in the north with a fleet of ten or more.", want: 20 },
  subscription: { name: "Standard", amount: "£5,000", start: "2026-01-01", term_months: 12 },
  onboardingSteps: "Kick-off call\nSend the welcome pack\nFirst review",
  agent: { name: "Scout", base_url: "https://llm.example/v1", model: "local", api_key: "a11y-not-a-key" },
  agreement: { name: "Master services" },
};
