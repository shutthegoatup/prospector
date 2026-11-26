import { parseSearchWith, stringifySearchWith } from "@tanstack/react-router";
import { z } from "zod";

// What each page reads from its address. Only a whole number or true/false is read as
// anything but text, and only when it prints back exactly as written, so ?q=1e5 and
// ?q=null stay text; ?task=3 arrives as the number 3. Text goes into the address as it
// is, so ?q=2024 stays that and not ?q="2024", and text reads a number or boolean back.
// A value that doesn't fit its schema is dropped for the page's default.

function readValue(raw: string): unknown {
  if (raw === "true" || raw === "false") return raw === "true";
  if (/^-?\d+$/.test(raw) && String(Number(raw)) === raw) return Number(raw);
  throw new Error("text");
}

export const parseSearch = parseSearchWith(readValue);
export const stringifySearch = stringifySearchWith(JSON.stringify);

const text = z
  .union([z.string(), z.number(), z.boolean()])
  .transform((v) => String(v))
  .optional()
  .catch(undefined);

// A flag is on when the address gives it a value other than 0 or false, as ?closed=1.
const flag = z
  .union([z.boolean(), z.number(), z.string()])
  .transform((v) => v !== false && v !== 0 && v !== "" && v !== "0" && v !== "false")
  .optional()
  .catch(undefined);

const id = z.number().int().positive().optional().catch(undefined);

function oneOf<const T extends readonly [string, ...string[]]>(values: T) {
  return z.enum(values).optional().catch(undefined);
}

export const todaySteps = ["call", "accepted", "email", "followup", "linkedin", "next"] as const;
export const researchFilters = ["review", "contact", "verify", "details", "rejected"] as const;
export const enquiryStates = ["open", "waiting", "closed"] as const;
export const enquiryKinds = ["sales", "demo", "signup", "support", "feature", "other"] as const;
export const pipelines = ["research", "outreach", "replied", "meeting", "proposal", "won", "lost", "rejected"] as const;

export const todaySearch = z.object({ step: oneOf(todaySteps) });

export const researchSearch = z.object({
  f: oneOf(researchFilters),
  task: id,
  // The offering whose queue this is; left out, the organisation's own.
  of: id,
});

export const enquiriesSearch = z.object({ state: oneOf(enquiryStates), kind: oneOf(enquiryKinds), spam: flag });

export const companiesSearch = z.object({ q: text, segment: text, stage: oneOf(pipelines) });

export const tasksSearch = z.object({ closed: flag });

export const addSearch = z.object({ popup: flag, url: text, name: text, headline: text, company: text });

// An id in a path is a positive whole number; anything else is no such page.
export const pathId = z.coerce.number().int().positive();
