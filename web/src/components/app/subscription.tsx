import { Link } from "@tanstack/react-router";
import { type ComponentProps, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import { defineMessage, defineMessages, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, useRefresh } from "@/components/app";
import { Input } from "@/components/ui/input";
import { intl } from "@/i18n";
import { onDay, today } from "@/lib/format";
import { useSession } from "@/session";

// A subscription as the Subscriptions page and a company's page both show and edit it.

type Subscription = components["schemas"]["Subscription"];
type Change = components["schemas"]["SubscriptionChange"];

const m = defineMessages({
  update: { defaultMessage: "Update", id: "ahRYK4", description: "Opens the form to change a subscription" },
  on: { defaultMessage: "What they're on", id: "960dTG", description: "Label of a subscription's name, such as the plan the customer is on" },
  price: { defaultMessage: "Price", id: "heKwYR", description: "Label of a subscription's price" },
  priceExample: { defaultMessage: "e.g. 14,000", id: "ovjeHu", description: "Example price shown in an empty price field" },
  per: { defaultMessage: "For", id: "Nfv3Zi", description: "What a screen reader calls the choice of a price's period" },
  aYear: { defaultMessage: "a year", id: "XNsziM", description: "A price's period: per year" },
  aMonth: { defaultMessage: "a month", id: "ablhav", description: "A price's period: per month" },
  started: { defaultMessage: "Started", id: "W+i2Rb", description: "Label of the date a subscription started" },
  committed: { defaultMessage: "Committed for, months", id: "knZaft", description: "Label of how many months a subscription is committed for" },
  atEnd: { defaultMessage: "At the end of the term", id: "UGydkf", description: "Label of what happens when a subscription's term ends" },
  notice: { defaultMessage: "Notice to end, days", id: "U4zyRn", description: "Label of how many days' notice ending a subscription takes" },
  noticeOn: { defaultMessage: "Notice given on", id: "E2mO80", description: "Label of the date notice to end was given" },
  endedOn: { defaultMessage: "Ended early on", id: "XhsQeO", description: "Label of the date a subscription ended before its term" },
  enterNumber: { defaultMessage: "Enter a number, or 0 for none.", id: "be0lfg", description: "When a number field of a subscription was emptied" },
  renewing: {
    defaultMessage: "Renewal under way in Conversations",
    id: "6gGSHy",
    description: "Link to the renewal deal of a subscription on the deals board",
  },
  renewalWith: { defaultMessage: "Renewal with", id: "Zxy8BI", description: "Small heading before choosing who a renewal deal is with" },
  renewalWho: { defaultMessage: "Who the renewal is with", id: "jawOeQ", description: "What a screen reader calls the choice of who a renewal deal is with" },
  startRenewal: { defaultMessage: "Start the renewal deal", id: "AjPv3Q", description: "Button to start a deal to renew a subscription" },
  noOneOnBoard: {
    defaultMessage: "To start a renewal deal, someone at this company needs to be on the Conversations board first.",
    id: "TZFIp7",
    description: "Why a renewal deal can't be started yet",
  },
  remove: { defaultMessage: "Remove: recorded by mistake", id: "4QQelz", description: "Button to delete a subscription recorded by mistake" },
});

const v = {
  afterTerm: defineMessage<{ after: string }>({
    defaultMessage: "{after, select, renews {renews for the same term} rolls {rolls on, a period at a time} other {ends}}",
    id: "Xf/BPe",
    description: "What a subscription does at the end of its term: renews for the same term, rolls on a period at a time, or ends",
  }),
  noCommitment: defineMessage<{ per: string }>({
    defaultMessage: "no commitment, {per, select, month {a month} other {a year}} at a time",
    id: "PlNJEO",
    description: "A subscription's term when it isn't committed for any months",
  }),
  term: defineMessage<{ months: number; after: string }>({
    defaultMessage: "{months, plural, one {# month} other {# months}}, then {after}",
    id: "wRJhmv",
    description: "A subscription's term: how many months it is committed for, then what happens",
  }),
  months: defineMessage<{ months: number }>({
    defaultMessage: "{months, plural, one {# month} other {# months}}",
    id: "KHQH/4",
    description: "A subscription's term: how many months it is committed for",
  }),
  ended: defineMessage<{ on: string }>({ defaultMessage: "ended {on}", id: "1Oa9ED", description: "When a subscription ended. {on} is a date" }),
  starts: defineMessage<{ on: string }>({ defaultMessage: "starts {on}", id: "jKRBkE", description: "When a scheduled subscription starts. {on} is a date" }),
  ends: defineMessage<{ on: string }>({ defaultMessage: "ends {on}", id: "r5Zsvr", description: "When a subscription ends. {on} is a date" }),
  nextCommitted: defineMessage<{ term: string; by: string; ends: string }>({
    defaultMessage: "term ends {term} and the next is committed to · cancel by {by} to end {ends}",
    id: "97gM+/",
    description: "Where a subscription stands when its next term is already committed to. Each placeholder is a date",
  }),
  standing: defineMessage<{ term: string; by: string }>({
    defaultMessage: "term ends {term} · cancel by {by}",
    id: "4iElTm",
    description: "Where a subscription stands: when its term ends and the last day to cancel. Each is a date",
  }),
};

const statuses = defineMessages({
  scheduled: { defaultMessage: "scheduled", id: "PjBuiK", description: "A subscription's status: it starts on a later day" },
  active: { defaultMessage: "active", id: "DMQIfb", description: "A subscription's status: running" },
  "notice given": { defaultMessage: "notice given", id: "eHEAep", description: "A subscription's status: the customer has given notice to end it" },
  ending: { defaultMessage: "ending", id: "hrqAIq", description: "A subscription's status: it ends at the end of its term" },
  ended: { defaultMessage: "ended", id: "tcIdgR", description: "A subscription's status: it has ended" },
});

// statusLabel is where a subscription stands, in the reader's words.
export function statusLabel(status: string) {
  return status in statuses ? intl.formatMessage(statuses[status as keyof typeof statuses]) : status;
}

// thens are what a subscription can do at the end of its term.
export const thens = ["renews", "rolls", "ends"] as const;

export function thenLabel(after: string) {
  return intl.formatMessage(v.afterTerm, { after });
}

const select = "h-8 rounded-lg border border-input bg-muted px-2 text-sm";

export function termText(s: Subscription) {
  if (s.term_months === 0) return intl.formatMessage(v.noCommitment, { per: s.per });
  const known = (thens as readonly string[]).includes(s.then);
  return known ? intl.formatMessage(v.term, { months: s.term_months, after: thenLabel(s.then) }) : intl.formatMessage(v.months, { months: s.term_months });
}

const on = (date: string) => onDay(date, today());

export function standingText(s: Subscription) {
  if (s.status === "ended") return intl.formatMessage(v.ended, { on: on(s.ends) });
  if (s.status === "scheduled") return intl.formatMessage(v.starts, { on: on(s.start) });
  if (s.ends) return intl.formatMessage(v.ends, { on: on(s.ends) });
  if (s.cancel_ends && s.cancel_ends !== s.term_end) {
    return intl.formatMessage(v.nextCommitted, { term: on(s.term_end), by: on(s.cancel_by), ends: on(s.cancel_ends) });
  }
  return intl.formatMessage(v.standing, { term: on(s.term_end), by: on(s.cancel_by) });
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
  const ref = useCommitted<HTMLSelectElement>((chosen) => save(name, chosen));
  return (
    <select ref={ref} defaultValue={value} aria-label={label} className={select}>
      {children}
    </select>
  );
}

const box = (key: keyof Change, value: string | number) => `${key}:${value}`;

export function SubscriptionUpdate({ s }: { s: Subscription }) {
  const say = useIntl().formatMessage;
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
      setProblem({ detail: say(m.enterNumber) });
      return;
    }
    change.mutate({ ...path, body: { [key]: number ? +value : value } });
  };
  // Each box is keyed on what the server holds, so one it rewrote (a price given its
  // sign, a term that now rolls) shows the rewrite after the refresh.

  return (
    <details className="mt-1 text-sm">
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">{say(m.update)}</summary>
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-[170px_1fr]">
        <Eyebrow>{say(m.on)}</Eyebrow>
        <Field key={box("name", s.name)} name="name" save={save} defaultValue={s.name} aria-label={say(m.on)} className="h-8 bg-muted" />
        <Eyebrow>{say(m.price)}</Eyebrow>
        <span className="flex items-center gap-2">
          <Field
            key={box("amount", s.figure)}
            name="amount"
            save={save}
            defaultValue={s.figure}
            placeholder={say(m.priceExample)}
            aria-label={say(m.price)}
            className="h-8 w-44 bg-muted"
          />
          <Choice key={box("per", s.per)} name="per" save={save} value={s.per === "month" ? "month" : "year"} label={say(m.per)}>
            <option value="year">{say(m.aYear)}</option>
            <option value="month">{say(m.aMonth)}</option>
          </Choice>
        </span>
        <Eyebrow>{say(m.started)}</Eyebrow>
        <Field
          key={box("start", s.start)}
          name="start"
          save={save}
          type="date"
          defaultValue={s.start}
          aria-label={say(m.started)}
          className="h-8 w-44 bg-muted"
        />
        <Eyebrow>{say(m.committed)}</Eyebrow>
        <Field
          key={box("term_months", s.term_months)}
          name="term_months"
          save={save}
          number
          defaultValue={s.term_months}
          aria-label={say(m.committed)}
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>{say(m.atEnd)}</Eyebrow>
        <Choice key={box("then", s.then)} name="then" save={save} value={s.then} label={say(m.atEnd)}>
          {thens.map((t) => (
            <option key={t} value={t}>
              {thenLabel(t)}
            </option>
          ))}
        </Choice>
        <Eyebrow>{say(m.notice)}</Eyebrow>
        <Field
          key={box("notice_days", s.notice_days)}
          name="notice_days"
          save={save}
          number
          defaultValue={s.notice_days}
          aria-label={say(m.notice)}
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>{say(m.noticeOn)}</Eyebrow>
        <Field
          key={box("notice_on", s.notice_on)}
          name="notice_on"
          save={save}
          type="date"
          defaultValue={s.notice_on}
          aria-label={say(m.noticeOn)}
          className="h-8 w-44 bg-muted"
        />
        <Eyebrow>{say(m.endedOn)}</Eyebrow>
        <Field
          key={box("ended_on", s.ended_on)}
          name="ended_on"
          save={save}
          type="date"
          defaultValue={s.ended_on}
          aria-label={say(m.endedOn)}
          className="h-8 w-44 bg-muted"
        />
      </div>
      <Failed errors={[problem, change.error]} />
      {s.renewing ? (
        <p className="mt-2 border-t border-border pt-2">
          <Link to="/o/$org/conversations" params={{ org }} className="font-mono text-xs text-mark underline">
            {say(m.renewing)}
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
          <Eyebrow>{say(m.renewalWith)}</Eyebrow>
          <select
            aria-label={say(m.renewalWho)}
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
            {say(m.startRenewal)}
          </ActionButton>
          <Failed errors={[renew.error]} />
        </form>
      ) : (
        s.status !== "ended" && <p className="mt-2 border-t border-border pt-2 font-mono text-xs text-muted-foreground">{say(m.noOneOnBoard)}</p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2">
        <ActionButton quiet size="sm" disabled={remove.isPending} onClick={() => remove.mutate(path)}>
          {say(m.remove)}
        </ActionButton>
        <Failed errors={[remove.error]} />
      </div>
    </details>
  );
}
