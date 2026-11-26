import { type ReactNode, useEffect, useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, Section, useRefresh } from "@/components/app";
import {
  CohortDot,
  type CohortRule,
  cohortColours,
  conversationStages,
  dealKinds,
  listOf,
  type RuleField,
  ruleFields,
  ruleText,
} from "@/components/app/cohort";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useOfferings } from "@/lib/offerings";
import { actor, plural } from "@/lib/format";
import { useSession } from "@/session";

type Cohort = components["schemas"]["Cohort"];
type Preview = components["schemas"]["CohortPreview"];
type Move = components["schemas"]["CohortMove"];

const field = "h-10 rounded-lg border border-input bg-muted px-3 text-sm disabled:opacity-60";
const area = "rounded-lg border border-input bg-muted px-3 py-2 text-sm leading-relaxed disabled:opacity-60";
const select = "h-10 rounded-lg border border-input bg-muted px-3 text-sm";

type Draft = { name: string; colour: string; who_they_are: string; rules: CohortRule[] };

function draftOf(c?: Cohort): Draft {
  return { name: c?.name ?? "", colour: c?.colour ?? "", who_they_are: c?.who_they_are ?? "", rules: c?.rules ?? [] };
}

function useOfferingName(orgName: string) {
  const offerings = useOfferings();
  return (id: number) => (id === 0 ? orgName : (offerings.find((o) => o.id === id)?.name ?? "a removed offering"));
}

export function Cohorts({ orgName, currency }: { orgName: string; currency: string }) {
  const { org, owner } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/cohorts", { params: { path: { org }, query: { limit: 200 } } });
  const [editing, setEditing] = useState<number | "new" | null>(null);
  const rows = list.data?.items ?? [];
  const catchAll = rows.find((c) => c.catch_all);
  return (
    <Section
      title="Cohorts"
      hint="The kinds of company you sell to. Each company is in one: the rules place it, top to bottom, the first that matches winning, and the last cohort takes whatever no rule does. Anyone can move a company by hand, and then the rules leave it be."
    >
      {list.data && rows.length === 0 && (
        <p className="text-sm text-muted-foreground">None yet, so every company is simply one of {orgName}'s. The first cohort you add takes them all.</p>
      )}
      <ol className="flex flex-col gap-2">
        {rows.map((c, i) =>
          editing === c.id ? (
            <li key={c.id}>
              <CohortEditor cohort={c} cohorts={rows} orgName={orgName} currency={currency} onDone={() => setEditing(null)} />
            </li>
          ) : (
            <CohortRow
              key={c.id}
              c={c}
              n={i + 1}
              canUp={i > 0 && !c.catch_all}
              canDown={!c.catch_all && i < rows.length - 2}
              orgName={orgName}
              currency={currency}
              onEdit={() => setEditing(c.id)}
            />
          ),
        )}
      </ol>
      {owner &&
        (editing === "new" ? (
          <CohortEditor cohorts={rows} orgName={orgName} currency={currency} onDone={() => setEditing(null)} />
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <ActionButton size="sm" onClick={() => setEditing("new")}>
              Add a cohort
            </ActionButton>
            <Meta>
              {catchAll
                ? `A new one goes just above ${catchAll.name}. You see who would move before you save.`
                : "You see who would move before you save."}
            </Meta>
          </div>
        ))}
      <Failed errors={[list.error]} />
    </Section>
  );
}

function CohortRow({
  c,
  n,
  canUp,
  canDown,
  orgName,
  currency,
  onEdit,
}: {
  c: Cohort;
  n: number;
  canUp: boolean;
  canDown: boolean;
  orgName: string;
  currency: string;
  onEdit: () => void;
}) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const move = $api.useMutation("put", "/o/{org}/api/v1/cohorts/{id}/position", { onSuccess: refresh });
  const offeringName = useOfferingName(orgName);
  const to = (position: number) => move.mutate({ params: { path: { org, id: c.id } }, body: { position } });
  const said = c.rules.map((r) => ruleText(r, currency, offeringName));
  let when = said.length > 0 ? "when " + said.join(", or ") : "no rules yet";
  if (c.catch_all) when = (said.length > 0 ? when + "; and " : "") + "every company no rule above takes · always last";
  return (
    <li className="flex flex-col gap-1.5 rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="flex min-w-0 flex-wrap items-center gap-2.5">
          <span className="font-mono text-xs text-muted-foreground">{n}</span>
          <CohortDot colour={c.colour} />
          <b className="font-heading">{c.name}</b>
          <span className="rounded-md bg-secondary px-2 py-0.5 font-mono text-xs">
            {plural(c.companies, "company", "companies")}
            {c.by_hand > 0 && ` · ${c.by_hand} by hand`}
          </span>
        </span>
        {owner && (
          <span className="flex items-center gap-1.5">
            {canUp && (
              <ActionButton quiet size="xs" aria-label={`Move ${c.name} up`} disabled={move.isPending} onClick={() => to(c.position - 1)}>
                ↑
              </ActionButton>
            )}
            {canDown && (
              <ActionButton quiet size="xs" aria-label={`Move ${c.name} down`} disabled={move.isPending} onClick={() => to(c.position + 1)}>
                ↓
              </ActionButton>
            )}
            <ActionButton quiet size="xs" onClick={onEdit}>
              Edit
            </ActionButton>
          </span>
        )}
      </div>
      <Meta className="text-foreground">{when}</Meta>
      {c.who_they_are && <p className="text-sm text-muted-foreground">{c.who_they_are}</p>}
      <Failed errors={[move.error]} />
    </li>
  );
}

function blankRule(f: RuleField): CohortRule {
  return { field: f, value: f === "billed" ? "monthly" : "" };
}

function Ticks({ options, value, onChange }: { options: { key: string; label: string }[]; value: string; onChange: (v: string) => void }) {
  const on = listOf(value);
  const toggle = (key: string, yes: boolean) => onChange(options.map((o) => o.key).filter((k) => (k === key ? yes : on.includes(k))).join(", "));
  return (
    <span className="flex flex-wrap items-center gap-3">
      {options.map((o) => (
        <label key={o.key} className="flex items-center gap-1.5 text-sm">
          <Checkbox checked={on.includes(o.key)} onCheckedChange={(yes) => toggle(o.key, yes === true)} />
          {o.label}
        </label>
      ))}
    </span>
  );
}

function RuleValue({ rule, orgName, currency, onChange }: { rule: CohortRule; orgName: string; currency: string; onChange: (v: string) => void }) {
  const offerings = useOfferings();
  switch (rule.field) {
    case "offering":
      return <Ticks options={[{ key: "0", label: orgName }, ...offerings.map((o) => ({ key: String(o.id), label: o.name }))]} value={rule.value} onChange={onChange} />;
    case "conversation_stage":
      return <Ticks options={conversationStages} value={rule.value} onChange={onChange} />;
    case "deal_kind":
      return <Ticks options={dealKinds} value={rule.value} onChange={onChange} />;
    case "billed":
      return (
        <select aria-label="Billed" value={rule.value} onChange={(e) => onChange(e.target.value)} className={select}>
          <option value="monthly">monthly</option>
          <option value="yearly">yearly</option>
        </select>
      );
    case "deal_value":
    case "pays_a_year":
      return (
        <span className="flex items-center gap-2 text-sm text-muted-foreground">
          <Input
            type="number"
            min={0}
            aria-label="Amount"
            value={rule.value === "" ? "" : Number(rule.value) / 100}
            onChange={(e) => onChange(e.target.value === "" ? "" : String(Math.round(Number(e.target.value) * 100)))}
            className={`${field} w-40`}
          />
          {currency}
          {rule.field === "pays_a_year" ? " a year, from live subscriptions" : ", open deals in it only"}
        </span>
      );
  }
  const hint: Partial<Record<RuleField, string>> = {
    segment: "manufacturer, OEM, carmaker, boatbuilder",
    country: "GB, IE",
    found_by: "agent:scout, import",
  };
  return <Input aria-label="Matches any of" value={rule.value} placeholder={hint[rule.field]} onChange={(e) => onChange(e.target.value)} className={`${field} flex-1`} />;
}

function names(moves: Move[]) {
  const shown = moves.slice(0, 8).map((m) => m.company);
  return shown.join(" · ") + (moves.length > shown.length ? ` and ${moves.length - shown.length} more` : "");
}

function PreviewSays({ p }: { p: Preview }) {
  const moved = p.moving_in.length + p.moving_out.length;
  return (
    <div className="flex flex-col gap-1 border-t border-dashed border-border pt-3">
      <Meta className="text-foreground">
        <b>{moved === 0 ? "Saving moves nobody" : `Saving moves ${p.moving_in.length} in, ${p.moving_out.length} out`}</b>
        {p.kept_by_hand.length > 0 && ` · ${plural(p.kept_by_hand.length, "company", "companies")} moved by hand stay where they are`}
      </Meta>
      {p.moving_in.length > 0 && <Meta>in: {names(p.moving_in)}</Meta>}
      {p.moving_out.length > 0 && (
        <Meta>
          out:{" "}
          {p.moving_out
            .slice(0, 8)
            .map((m) => `${m.company} → ${m.to.name}`)
            .join(" · ")}
          {p.moving_out.length > 8 && ` and ${p.moving_out.length - 8} more`}
        </Meta>
      )}
      {p.kept_by_hand.length > 0 && <Meta>staying by hand: {names(p.kept_by_hand)}</Meta>}
      {p.other_currency_deals > 0 && <Meta>{plural(p.other_currency_deals, "open deal")} in another currency can't be weighed by a deal rule.</Meta>}
    </div>
  );
}

function Labelled({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <Eyebrow>{label}</Eyebrow>
      {children}
    </label>
  );
}

function CohortEditor({
  cohort,
  cohorts,
  orgName,
  currency,
  onDone,
}: {
  cohort?: Cohort;
  cohorts: Cohort[];
  orgName: string;
  currency: string;
  onDone: () => void;
}) {
  const { org } = useSession();
  const refresh = useRefresh();
  const [draft, setDraft] = useState<Draft>(() => draftOf(cohort));
  const preview = $api.useMutation("post", "/o/{org}/api/v1/cohorts/preview");
  const done = () => {
    refresh();
    onDone();
  };
  const add = $api.useMutation("post", "/o/{org}/api/v1/cohorts", { onSuccess: done });
  const change = $api.useMutation("patch", "/o/{org}/api/v1/cohorts/{id}", { onSuccess: done });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/cohorts/{id}", { onSuccess: done });
  const body = { name: draft.name, colour: draft.colour || undefined, who_they_are: draft.who_they_are, rules: draft.rules };
  const ready = draft.name.trim() !== "";
  const { mutate } = preview;
  const asked = JSON.stringify(body);
  useEffect(() => {
    if (!ready) return;
    const wait = setTimeout(() => mutate({ params: { path: { org } }, body: { cohort: cohort?.id ?? 0, change: JSON.parse(asked) } }), 350);
    return () => clearTimeout(wait);
  }, [asked, ready, org, cohort?.id, mutate]);
  const setRule = (i: number, r: CohortRule) => setDraft({ ...draft, rules: draft.rules.map((x, j) => (j === i ? r : x)) });
  const save = () =>
    cohort ? change.mutate({ params: { path: { org, id: cohort.id } }, body }) : add.mutate({ params: { path: { org } }, body: { ...body, name: draft.name } });
  const others = cohorts.filter((c) => c.id !== cohort?.id);
  const goes = cohort?.catch_all ? others[others.length - 1]?.name : cohorts.find((c) => c.catch_all)?.name;
  const removeSays = !cohort
    ? ""
    : others.length === 0
      ? `Remove ${cohort.name}? It is the only cohort, so every company goes back to being in none.`
      : `Remove ${cohort.name}? Its ${plural(cohort.companies, "company", "companies")}, those moved here by hand too, are placed again by the other cohorts' rules, and any no rule takes go to ${goes}.`;
  const p = preview.data;
  const moves = p ? p.moving_in.length + p.moving_out.length : 0;
  return (
    <form
      className="flex flex-col gap-4 rounded-lg border-2 border-ink bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Labelled label="Name">
        <Input required maxLength={60} value={draft.name} placeholder="Manufacturers" onChange={(e) => setDraft({ ...draft, name: e.target.value })} className={field} />
      </Labelled>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1">
          <Eyebrow>Colour</Eyebrow>
        </legend>
        <span className="flex flex-wrap gap-2">
          {cohortColours.map((colour) => (
            <button
              key={colour}
              type="button"
              aria-label={colour}
              aria-pressed={draft.colour === colour}
              onClick={() => setDraft({ ...draft, colour })}
              className={`size-7 cursor-pointer rounded-md border-2 ${draft.colour === colour ? "border-ink ring-2" : "border-border"}`}
              style={{ background: colour }}
            />
          ))}
        </span>
        {draft.colour === "" && <Meta>Left unpicked, it gets one no other cohort has.</Meta>}
      </fieldset>
      <Labelled label="Who they are">
        <Textarea
          rows={3}
          maxLength={1000}
          value={draft.who_they_are}
          placeholder="Makers of cars, boats or machines with options to configure, who sell through dealers and a website."
          onChange={(e) => setDraft({ ...draft, who_they_are: e.target.value })}
          className={area}
        />
        <Meta>Research agents are told this, and asked to start each company's segment with what it is, so a segment rule has something to match.</Meta>
      </Labelled>
      <div className="flex flex-col gap-2">
        <Eyebrow>Who's in it: any of these</Eyebrow>
        {draft.rules.length === 0 && (
          <Meta>{cohort?.catch_all ? "No rules: it takes every company no cohort above does." : "No rules yet, so only companies moved here by hand."}</Meta>
        )}
        {draft.rules.map((r, i) => (
          <div key={i} className="flex flex-wrap items-center gap-2 border-b border-dashed border-border pb-1.5">
            <select
              aria-label="What it looks at"
              value={r.field}
              onChange={(e) => setRule(i, blankRule(e.target.value as RuleField))}
              className={select}
            >
              {ruleFields.map((f) => (
                <option key={f.field} value={f.field}>
                  {f.label}
                </option>
              ))}
            </select>
            <RuleValue rule={r} orgName={orgName} currency={currency} onChange={(value) => setRule(i, { field: r.field, value })} />
            <ActionButton
              quiet
              type="button"
              size="xs"
              aria-label="Remove this rule"
              onClick={() => setDraft({ ...draft, rules: draft.rules.filter((_, j) => j !== i) })}
            >
              ×
            </ActionButton>
          </div>
        ))}
        {draft.rules.some((r) => r.field === "segment") && (
          <Meta>A segment is read for what the company is: "CGI studio for carmakers" is a studio. A word matches any starting with it.</Meta>
        )}
        <div>
          <ActionButton quiet type="button" size="xs" onClick={() => setDraft({ ...draft, rules: [...draft.rules, blankRule("segment")] })}>
            Add a rule
          </ActionButton>
        </div>
      </div>
      {ready && p && <PreviewSays p={p} />}
      <Failed errors={[preview.error]} />
      <div className="flex flex-wrap items-center gap-2">
        <ActionButton type="submit" size="sm" disabled={!ready || add.isPending || change.isPending}>
          {moves > 0 ? `Save and move ${plural(moves, "company", "companies")}` : "Save"}
        </ActionButton>
        <ActionButton quiet type="button" size="sm" onClick={onDone}>
          Cancel
        </ActionButton>
        {cohort && (
          <ActionButton
            quiet
            type="button"
            size="sm"
            className="ml-auto"
            disabled={remove.isPending}
            onClick={() => window.confirm(removeSays) && remove.mutate({ params: { path: { org, id: cohort.id } } })}
          >
            Remove
          </ActionButton>
        )}
      </div>
      {cohort && <Meta>Changed by {actor(cohort.updated_by)}</Meta>}
      <Failed errors={[add.error, change.error, remove.error]} />
    </form>
  );
}
