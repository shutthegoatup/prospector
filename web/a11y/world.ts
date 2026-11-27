// Who and what the accessibility run's server holds: invented, on .example domains.
export const port = Number(process.env.A11Y_PORT ?? 8791);
export const base = `http://127.0.0.1:${port}`;
export const member = { email: "robin@prospector.example", name: "Robin Ash", password: "a11y-correct-horse-battery" };
export const org = { slug: "alpha", name: "Alpha Widgets" };
// A member who does neither area's work, so every page shows them what only reading is.
export const reader = { email: "sky@prospector.example", name: "Sky Reed", password: "a11y-reader-battery-staple" };
