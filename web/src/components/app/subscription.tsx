import { Link } from "@tanstack/react-router";
import { type ComponentProps, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, useRefresh } from "@/components/app";
import { Input } from "@/components/ui/input";
import { plural } from "@/lib/format";
import { useSession } from "@/session";

// A subscription as the Subscriptions page and a company's page both show and edit it.

type Subscription = components["schemas"]["Subscription"];
type Change = components["schemas"]["SubscriptionChange"];

export const thens = [
  { key: "renews", label: "renews for the same term" },
  { key: "rolls", label: "rolls on, a period at a time" },
  { key: "ends", label: "ends" },
];

const select = "h-8 rounded-lg border border-input bg-muted px-2 text-sm";

export function termText(s: Subscription) {
  if (s.term_months === 0) return `no commitment, a ${s.per} at a time`;
  const then = thens.find((t) => t.key === s.then);
  return plural(s.term_months, "month") + (then ? ", then " + then.label : "");
}

export function standingText(s: Subscription) {
  if (s.status === "ended") return "ended " + s.ends;
  if (s.status === "scheduled") return "starts " + s.start;
  if (s.ends) return "ends " + s.ends;
  if (s.cancel_ends && s.cancel_ends !== s.term_end) {
    return `term ends ${s.term_end} and the next is committed to · cancel by ${s.cancel_by} to end ${s.cancel_ends}`;
  }
  return `term ends ${s.term_end} · cancel by ${s.cancel_by}`;
}

// committed calls back on the DOM's change event, when an edit is done, as the
// server-rendered page saves: React's onChange fires on every keystroke.
function useCommitted<T extends HTMLInputElement | HTMLSelectElement>(save: (value: string, el: T) => void) {
  const ref = useRef<T>(null);
  const latest = useRef(save);
  // Kept current after each render, never during one, and before any event can read it.
  useLayoutEffect(() => {
    latest.current = save;
  });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const changed = () => latest.current(el.value, el);
    el.addEventListener("change", changed);
    return () => el.removeEventListener("change", changed);
  }, []);
  return ref;
}

type Save = (key: keyof Change, value: string, number?: boolean) => void;

function Field({ name, save, number, ...props }: ComponentProps<typeof Input> & { name: keyof Change; save: Save; number?: boolean }) {
  const ref = useCommitted<HTMLInputElement>((value) => save(name, value, number));
  return <Input ref={ref} {...props} type={number ? "number" : props.type} />;
}

function Choice({ name, save, value, children, label }: { name: keyof Change; save: Save; value: string; children: ReactNode; label: string }) {
  const ref = useCommitted<HTMLSelectElement>((v) => save(name, v));
  return (
    <select ref={ref} defaultValue={value} aria-label={label} className={select}>
      {children}
    </select>
  );
}

export function SubscriptionUpdate({ s }: { s: Subscription }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const path = { params: { path: { org, id: s.id } } };
  const [problem, setProblem] = useState<{ detail: string } | null>(null);
  const change = $api.useMutation("patch", "/o/{org}/api/v1/subscriptions/{id}", { onSuccess: refresh });
  const renew = $api.useMutation("post", "/o/{org}/api/v1/subscriptions/{id}/renewals", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/subscriptions/{id}", { onSuccess: refresh });
  const [who, setWho] = useState<number | undefined>(s.people[0]?.id);

  const save: Save = (key, value, number) => {
    setProblem(null);
    change.reset();
    // An emptied number box is not a nought: saving one would end a commitment.
    if (number && value.trim() === "") {
      setProblem({ detail: "Enter a number, or 0 for none." });
      return;
    }
    change.mutate({ ...path, body: { [key]: number ? +value : value } });
  };
  // Each box is keyed on what the server holds, so one it rewrote (a price given its
  // sign, a term that now rolls) shows the rewrite after the refresh.
  const box = (key: keyof Change, value: string | number) => `${key}:${value}`;

  return (
    <details className="mt-1 text-sm">
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">Update</summary>
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-[170px_1fr]">
        <Eyebrow>What they're on</Eyebrow>
        <Field key={box("name", s.name)} name="name" save={save} defaultValue={s.name} aria-label="What they're on" className="h-8 bg-muted" />
        <Eyebrow>Price</Eyebrow>
        <span className="flex items-center gap-2">
          <Field
            key={box("amount", s.figure)}
            name="amount"
            save={save}
            defaultValue={s.figure}
            placeholder="e.g. 14,000"
            aria-label="Price"
            className="h-8 w-44 bg-muted"
          />
          <Choice key={box("per", s.per)} name="per" save={save} value={s.per === "month" ? "month" : "year"} label="For">
            <option value="year">a year</option>
            <option value="month">a month</option>
          </Choice>
        </span>
        <Eyebrow>Started</Eyebrow>
        <Field key={box("start", s.start)} name="start" save={save} type="date" defaultValue={s.start} aria-label="Started" className="h-8 w-44 bg-muted" />
        <Eyebrow>Committed for, months</Eyebrow>
        <Field
          key={box("term_months", s.term_months)}
          name="term_months"
          save={save}
          number
          defaultValue={s.term_months}
          aria-label="Committed for, months"
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>At the end of the term</Eyebrow>
        <Choice key={box("then", s.then)} name="then" save={save} value={s.then} label="At the end of the term">
          {thens.map((t) => (
            <option key={t.key} value={t.key}>
              {t.label}
            </option>
          ))}
        </Choice>
        <Eyebrow>Notice to end, days</Eyebrow>
        <Field
          key={box("notice_days", s.notice_days)}
          name="notice_days"
          save={save}
          number
          defaultValue={s.notice_days}
          aria-label="Notice to end, days"
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>Notice given on</Eyebrow>
        <Field
          key={box("notice_on", s.notice_on)}
          name="notice_on"
          save={save}
          type="date"
          defaultValue={s.notice_on}
          aria-label="Notice given on"
          className="h-8 w-44 bg-muted"
        />
        <Eyebrow>Ended early on</Eyebrow>
        <Field
          key={box("ended_on", s.ended_on)}
          name="ended_on"
          save={save}
          type="date"
          defaultValue={s.ended_on}
          aria-label="Ended early on"
          className="h-8 w-44 bg-muted"
        />
      </div>
      <Failed errors={[problem, change.error]} />
      {s.renewing ? (
        <p className="mt-2 border-t border-border pt-2">
          <Link to="/o/$org/conversations" params={{ org }} className="font-mono text-xs text-mark underline">
            Renewal under way in Conversations
          </Link>
        </p>
      ) : s.status !== "ended" && s.people.length > 0 ? (
        <form
          className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (who) renew.mutate({ ...path, body: { contact: who } });
          }}
        >
          <Eyebrow>Renewal with</Eyebrow>
          <select
            aria-label="Who the renewal is with"
            value={who}
            onChange={(e) => setWho(Number(e.target.value))}
            className="h-9 rounded-lg border border-input bg-muted px-2 text-sm"
          >
            {s.people.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <ActionButton quiet size="sm" type="submit" disabled={renew.isPending}>
            Start the renewal deal
          </ActionButton>
          <Failed errors={[renew.error]} />
        </form>
      ) : (
        s.status !== "ended" && (
          <p className="mt-2 border-t border-border pt-2 font-mono text-xs text-muted-foreground">
            To start a renewal deal, someone at this company needs to be on the Conversations board first.
          </p>
        )
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2">
        <ActionButton quiet size="sm" disabled={remove.isPending} onClick={() => remove.mutate(path)}>
          Remove: recorded by mistake
        </ActionButton>
        <Failed errors={[remove.error]} />
      </div>
    </details>
  );
}
