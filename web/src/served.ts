import { z } from "zod/mini";

// A page the server has already decided comes in the shell's data-page, for the pages
// with no API to ask: an error, starting a first organisation, a customer's onboarding
// and an unsubscribe link. It is drawn in place of whatever the address would route to.

const problem = z.object({ page: z.literal("problem"), code: z.number(), title: z.string(), message: z.string(), back: z.string() });

const start = z.object({ page: z.literal("start") });

const unsubscribe = z.object({ page: z.literal("unsubscribe"), token: z.optional(z.string()), done: z.boolean() });

export const customerStep = z.object({
  id: z.number(),
  name: z.string(),
  due: z.nullable(z.string()),
  done_on: z.nullable(z.string()),
  // Whether the customer marked it themselves, so they may take the mark back.
  mine: z.boolean(),
});

const customer = z.object({
  page: z.literal("customer"),
  state: z.catch(z.enum(["open", "again", "sent", "steps", "none"]), "none"),
  token: z.optional(z.string()),
  org: z.optional(z.string()),
  email: z.optional(z.string()),
  day: z.optional(z.string()),
  steps: z.array(customerStep),
});

export const servedSchema = z.discriminatedUnion("page", [problem, start, unsubscribe, customer]);

export type Served = z.infer<typeof servedSchema>;
export type CustomerStep = z.infer<typeof customerStep>;

// parseServed reads a data-page attribute; anything unreadable is no served page, and
// the address routes as usual.
export function parseServed(raw: string | undefined): Served | undefined {
  if (!raw) return undefined;
  try {
    const got = servedSchema.safeParse(JSON.parse(raw));
    return got.success ? got.data : undefined;
  } catch {
    return undefined;
  }
}

// stepMeta is what the note beside a customer's step says: when it was done, or when it
// is or was due, the page's day being today.
export function stepMeta(st: CustomerStep, day: string | undefined): { said: "done" | "late" | "due"; on: string } | undefined {
  if (st.done_on) return { said: "done", on: st.done_on };
  if (st.due && day && st.due < day) return { said: "late", on: st.due };
  if (st.due) return { said: "due", on: st.due };
  return undefined;
}
