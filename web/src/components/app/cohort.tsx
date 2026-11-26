import type { components } from "@/api";
import { cn } from "@/lib/utils";

type CohortRef = components["schemas"]["CohortRef"];
export type CohortRule = components["schemas"]["CohortRule"];
export type RuleField = CohortRule["field"];

// The palette a cohort's colour comes from, as the server holds it.
export const cohortColours = ["#7b8cff", "#ff9f43", "#35c58a", "#f06595", "#a77bff", "#22b8cf", "#e0b43c", "#b08968"];

export function CohortDot({ colour, className }: { colour: string; className?: string }) {
  return <span aria-hidden className={cn("inline-block shrink-0 rounded-sm", className)} style={{ background: colour, width: 12, height: 12 }} />;
}

// CohortChip is the cohort a company is in. One the catch-all took because no rule
// matched is dashed and asks, since that placement is a guess.
export function CohortChip({ cohort, why, byHand }: { cohort: CohortRef; why: string; byHand: boolean }) {
  if (cohort.id === 0) return null;
  const unsure = !byHand && why === "";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 font-mono text-xs text-foreground",
        unsure ? "border-dashed border-ink bg-card" : "border-border bg-secondary",
      )}
      title={unsure ? "No rule matched: the catch-all took it" : byHand ? "Put here by hand" : "By rule: " + why}
    >
      <CohortDot colour={cohort.colour} />
      {cohort.name}
      {unsure && "?"}
    </span>
  );
}

const currencySigns: Record<string, string> = { GBP: "£", EUR: "€", USD: "$" };

// money writes hundredths as the server's pages do.
export function money(hundredths: number, currency: string) {
  const whole = Math.floor(hundredths / 100).toLocaleString("en-GB");
  const cents = hundredths % 100;
  const figure = cents ? `${whole}.${String(cents).padStart(2, "0")}` : whole;
  const sign = currencySigns[currency];
  return sign ? sign + figure : `${currency} ${figure}`;
}

export const ruleFields: { field: RuleField; label: string }[] = [
  { field: "segment", label: "Segment says" },
  { field: "country", label: "Country is" },
  { field: "offering", label: "Being sold" },
  { field: "conversation_stage", label: "Conversation at" },
  { field: "deal_value", label: "Open deal of at least" },
  { field: "pays_a_year", label: "Pays a year at least" },
  { field: "billed", label: "Billed" },
  { field: "deal_kind", label: "Kind of deal" },
  { field: "found_by", label: "Found by" },
];

export const conversationStages = [
  { key: "replied", label: "Replied" },
  { key: "meeting", label: "Meeting" },
  { key: "proposal", label: "Proposal" },
  { key: "won", label: "Won" },
  { key: "lost", label: "Lost" },
];

export const dealKinds = [
  { key: "new", label: "New business" },
  { key: "renewal", label: "Renewal" },
  { key: "expansion", label: "Expansion" },
];

export function listOf(value: string) {
  return value
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}

function either(values: string[]) {
  if (values.length <= 1) return values.join("");
  return values.slice(0, -1).join(", ") + " or " + values[values.length - 1];
}

// ruleText says what a rule takes, in words.
export function ruleText(r: CohortRule, currency: string, offeringName: (id: number) => string) {
  const values = listOf(r.value);
  const label = (list: { key: string; label: string }[]) => values.map((v) => list.find((x) => x.key === v)?.label.toLowerCase() ?? v);
  switch (r.field) {
    case "segment":
      return `the segment says ${either(values)}`;
    case "country":
      return `in ${either(values)}`;
    case "offering":
      return `being sold ${either(values.map((v) => offeringName(Number(v))))}`;
    case "conversation_stage":
      return `a conversation at ${either(label(conversationStages))}`;
    case "deal_value":
      return `an open deal of at least ${money(Number(r.value), currency)}`;
    case "pays_a_year":
      return `pays at least ${money(Number(r.value), currency)} a year`;
    case "billed":
      return `billed ${r.value}`;
    case "deal_kind":
      return `a ${either(label(dealKinds))} deal`;
    case "found_by":
      return `found by ${either(values)}`;
  }
}
