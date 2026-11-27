import { defineMessage, defineMessages } from "react-intl";
import { intl } from "@/i18n";

// How pages write people, times and counts, in the language the app speaks.

const m = defineMessages({
  customer: { id: "format.actor.customer", defaultMessage: "the customer", description: "Who did something, when it was the customer themselves" },
  justNow: { id: "format.ago.justNow", defaultMessage: "just now", description: "Less than a minute ago" },
});

const agent = defineMessage<{ name: string }>({
  id: "format.actor.agent",
  defaultMessage: "agent · {name}",
  description: "Who did something, when it was one of the organisation's AI agents",
});

export function actor(by: string) {
  if (by === "customer") return intl.formatMessage(m.customer);
  return by.startsWith("agent:") ? intl.formatMessage(agent, { name: by.slice(6) }) : by;
}

export function day(at: string) {
  return intl.formatDate(at, { day: "numeric", month: "short" });
}

export function stamp(at: string) {
  return intl.formatDate(at, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}

// dated is a moment with its year.
export function dated(at: string) {
  return intl.formatDate(at, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function plural(n: number, one: string, many = one + "s") {
  return `${n} ${n === 1 ? one : many}`;
}

export function clock(at: string) {
  return intl.formatTime(at, { hour: "2-digit", minute: "2-digit" });
}

// ago is how long since: past a day, the date.
export function ago(at: string, now = Date.now()) {
  const ms = now - new Date(at).getTime();
  if (ms < 60_000) return intl.formatMessage(m.justNow);
  if (ms < 3_600_000) return intl.formatRelativeTime(-Math.floor(ms / 60_000), "minute", { style: "short" });
  if (ms < 86_400_000) return intl.formatRelativeTime(-Math.floor(ms / 3_600_000), "hour");
  return intl.formatDate(at, { day: "numeric", month: "short", year: "numeric" });
}
