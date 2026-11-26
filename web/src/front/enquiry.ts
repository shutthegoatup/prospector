export const hostedPrice = "£5";
const perUser = 5;

export function seatTotal(users: string): string {
  const n = Math.max(1, Math.min(500, Math.round(Number(users) || 1)));
  return `£${(n * perUser).toLocaleString("en-GB")} a month for ${n} ${n === 1 ? "user" : "users"}`;
}

// The server may be reached at an address its organisation hasn't listed, where every
// enquiry would be refused: the form shows only where it can send.
export function formShows(intake: string, origin: string, here: string): boolean {
  return intake !== "" && origin !== "" && origin === here;
}

export type Outcome = "sent" | "invalid" | "busy" | "refused" | "failed";

export const outcomeText: Record<Outcome, string> = {
  sent: "Sent. Someone will write back to you.",
  invalid: "That couldn't be taken. Check the email address and try again.",
  busy: "Too many messages just now. Your message is still here: try again in an hour.",
  refused: "This form can't send from here at the moment.",
  failed: "It didn't send. Your message is still here: try again in a minute.",
};

export function outcomeOf(status: number): Outcome | undefined {
  if (status >= 200 && status < 300) return "sent";
  if (status === 400) return "invalid";
  if (status === 429) return "busy";
  if (status < 500) return "refused";
  return undefined;
}

// The server keeps a repeated enquiry once, so a dropped answer is safe to send again.
// An answer that says no would say no again.
export async function send(to: string, body: string, wait = (ms: number) => new Promise((done) => setTimeout(done, ms))): Promise<Outcome> {
  for (let attempt = 0; attempt < 3; attempt++) {
    if (attempt) await wait(attempt * 1500);
    try {
      const res = await fetch(to, { method: "POST", headers: { "content-type": "application/json" }, body });
      const outcome = outcomeOf(res.status);
      if (outcome) return outcome;
    } catch {
      // offline or cut off: try again
    }
  }
  return "failed";
}
