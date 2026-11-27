import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { defineMessage, defineMessages, useIntl, type IntlShape } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, Meta, PageHead, Pill, type Tone, useRefresh, Whose } from "@/components/app";
import { Bar, Placeholders, placeholders } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { actor, onDay, today } from "@/lib/format";
import { useSession } from "@/session";

type Onboarding = components["schemas"]["Onboarding"];
type Step = components["schemas"]["OnboardingStep"];

// Between a company's name and its onboarding's, as everywhere: a mark, not a word.
const joiner = " · ";

const m = defineMessages({
  title: { defaultMessage: "Onboarding", id: "iR6303", description: "Title of the page of new customers being set up" },
  what: {
    defaultMessage:
      "The steps between a signature and a live customer. Each onboarding is a copy of your steps, made the day it starts, so changing them in Settings later leaves the ones running as they are. Start one from a company's page. Ones running late come first.",
    id: "hG1D2X",
    description: "What the onboarding page is, under its title",
  },
  whose: {
    defaultMessage: "Onboarding is support work, so here you read it.",
    id: "RHiXhj",
    description: "For a member who doesn't do support work, on the onboarding page",
  },
  noSteps: { defaultMessage: "No steps written yet", id: "7F4Sps", description: "When the organisation has written no onboarding steps" },
  noStepsHint: {
    defaultMessage: "An owner writes the steps under Settings, one a line. Then start an onboarding from a company's page.",
    id: "kQaFXv",
    description: "Under 'No steps written yet'",
  },
  nobody: { defaultMessage: "Nobody is being onboarded", id: "Wa0eMr", description: "When no onboarding is running" },
  nobodyHint: { defaultMessage: "Open a company and start one.", id: "DtEQxO", description: "Under 'Nobody is being onboarded'" },
  onTrack: { defaultMessage: "on track", id: "4mDVKP", description: "An onboarding with nothing late or due today" },
  noDate: { defaultMessage: "no date", id: "CBHS4N", description: "An onboarding step with no due date" },
  due: { defaultMessage: "Due", id: "bl7mMD", description: "What a screen reader calls a step's due date field" },
  done: { defaultMessage: "Done", id: "qitIYd", description: "Button marking an onboarding step done" },
  notDone: { defaultMessage: "Not done", id: "Z/1XxQ", description: "Button taking back an onboarding step's being done" },
  theirPage: { defaultMessage: "Their page", id: "n1er1P", description: "Small heading of the part about a customer's own page" },
  theirPageWhat: {
    defaultMessage:
      "Someone you invite is emailed a link to a page showing the steps that are theirs, which they can mark done, and nothing else. It needs no password and opens in one browser at a time.",
    id: "COjdHd",
    description: "What inviting a customer to their own page does",
  },
  withdrawn: { defaultMessage: "withdrawn", id: "4iTVHH", description: "A customer whose access to their page was withdrawn" },
  canOpen: { defaultMessage: "can open it", id: "g2DMOw", description: "A customer who can open their page" },
  withdraw: { defaultMessage: "Withdraw", id: "vk2Kcb", description: "Button withdrawing a customer's access to their page" },
  noneInvited: { defaultMessage: "Nobody has been invited yet.", id: "BEgYdK", description: "When no customer has been invited to their page" },
  noMail: {
    defaultMessage: "To invite anyone, an owner sets a mail server in Settings and the server needs its public address (PROSPECTOR_URL).",
    id: "i9Qbxx",
    description: "Why nobody can be invited: mail isn't set up",
  },
  noAddress: {
    defaultMessage: "Nobody at this company has an email address here to send a link to.",
    id: "jRA7LR",
    description: "Why nobody can be invited: no email addresses",
  },
  whoToInvite: { defaultMessage: "Who to invite", id: "gSfVRQ", description: "What a screen reader calls the choice of who to invite" },
  emailLink: { defaultMessage: "Email them a link", id: "nppeRC", description: "Button mailing a customer the link to their page" },
  sent: { defaultMessage: "Sent.", id: "I+sw6j", description: "After a customer's link was mailed" },
  removeConfirm: {
    defaultMessage: "Remove this onboarding and its steps? Use this for one started by mistake.",
    id: "z1RTXa",
    description: "Asked before removing an onboarding",
  },
  remove: { defaultMessage: "Remove", id: "xGCq8P", description: "Button removing an onboarding" },
});

const v = {
  live: defineMessage<{ on: string }>({ defaultMessage: "live since {on}", id: "B/3qsk", description: "An onboarding that has finished. {on} is a date" }),
  late: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# step} other {# steps}} late",
    id: "RKIbNa",
    description: "How many of an onboarding's steps are late",
  }),
  dueToday: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# step} other {# steps}} due today",
    id: "D7MX2J",
    description: "How many of an onboarding's steps are due today",
  }),
  doneOn: defineMessage<{ on: string }>({ defaultMessage: "done {on}", id: "Z1+Ad9", description: "When an onboarding step was done. {on} is a date" }),
  doneBy: defineMessage<{ on: string; who: string }>({
    defaultMessage: "done {on} · {who}",
    id: "WGhG+1",
    description: "When an onboarding step was done, and by whom. {on} is a date",
  }),
  wasDue: defineMessage<{ on: string }>({
    defaultMessage: "late, was due {on}",
    id: "7b0Vu/",
    description: "An onboarding step past its due date. {on} is a date",
  }),
  dueOn: defineMessage<{ on: string }>({ defaultMessage: "due {on}", id: "vf6SN/", description: "When an onboarding step is due. {on} is a date" }),
  progress: defineMessage<{ done: number; total: number; on: string }>({
    defaultMessage: "{done} of {total} · started {on}",
    id: "rgzXkp",
    description: "How many of an onboarding's steps are done, and when it started. {on} is a date",
  }),
  who: defineMessage<{ seq: number; who: string }>({
    defaultMessage: "{seq} · {who, select, them {them} other {us}} · ",
    id: "PO4dho",
    description: "Before an onboarding step's name: its number, and whose it is to do, the customer's (them) or ours (us)",
  }),
  withdrawConfirm: defineMessage<{ name: string }>({
    defaultMessage: "Withdraw {name}'s access? Their page closes in every browser at once.",
    id: "e8uBxQ",
    description: "Asked before withdrawing a customer's access to their page",
  }),
  invitee: defineMessage<{ name: string; email: string }>({
    defaultMessage: "{name} · {email}",
    id: "t5mCpG",
    description: "Someone who can be invited, in the list to choose from",
  }),
};

function standing(say: IntlShape["formatMessage"], o: Onboarding, day: string): [string, Tone] {
  const dueToday = o.steps.filter((s) => !s.done_on && s.due === day).length;
  if (o.live_on) return [say(v.live, { on: onDay(o.live_on, day) }), "ok"];
  if (o.late > 0) return [say(v.late, { n: o.late }), "stop"];
  if (dueToday > 0) return [say(v.dueToday, { n: dueToday }), "warn"];
  return [say(m.onTrack), "agent"];
}

function stepMeta(say: IntlShape["formatMessage"], s: Step, day: string) {
  if (s.done_on) return s.done_by ? say(v.doneBy, { on: onDay(s.done_on, day), who: actor(s.done_by) }) : say(v.doneOn, { on: onDay(s.done_on, day) });
  if (s.due && s.due < day) return say(v.wasDue, { on: onDay(s.due, day) });
  if (s.due) return say(v.dueOn, { on: onDay(s.due, day) });
  return say(m.noDate);
}

export function OnboardingPage() {
  const say = useIntl().formatMessage;
  const { org, support } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/onboardings", { params: { path: { org }, query: { limit: 200 } } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const planned = (settings.data?.onboarding_steps ?? "").split("\n").filter((l) => l.trim()).length;
  const day = today();
  // The cards wait for the settings too, as "No steps written yet" would go above them.
  const loading = (!list.data || settings.isPending) && !list.error && !settings.error;
  return (
    <>
      <PageHead title={say(m.title)} />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">{say(m.what)}</p>
      <Whose area="support" said={say(m.whose)} can={support} />
      <Failed errors={[list.error, settings.error]} />
      {!loading && settings.data && planned === 0 && <Empty title={say(m.noSteps)} hint={say(m.noStepsHint)} />}
      {!loading && settings.data && planned > 0 && list.data && list.data.items.length === 0 && <Empty title={say(m.nobody)} hint={say(m.nobodyHint)} />}
      {loading ? (
        <Placeholders className="flex max-w-3xl flex-col gap-3">
          {Array.from({ length: placeholders }, (_, i) => (
            <OnboardingCard key={i} day={day} />
          ))}
        </Placeholders>
      ) : (
        <div className="flex max-w-3xl flex-col gap-3">
          {list.data?.items.map((o) => (
            <OnboardingCard key={o.id} o={o} day={day} />
          ))}
        </div>
      )}
    </>
  );
}

// OnboardingCard is one onboarding, or its outline while the list loads; on its
// company's own page it goes unnamed.
export function OnboardingCard({ o, day, named = true }: { o?: Onboarding; day: string; named?: boolean }) {
  const say = useIntl().formatMessage;
  const { org, support } = useSession();
  const refresh = useRefresh();
  const step = $api.useMutation("patch", "/o/{org}/api/v1/onboarding-steps/{id}", { onSuccess: refresh });
  const invite = $api.useMutation("post", "/o/{org}/api/v1/onboardings/{id}/invites", { onSuccess: refresh });
  const withdraw = $api.useMutation("delete", "/o/{org}/api/v1/onboarding-invites/{id}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/onboardings/{id}", { onSuccess: refresh });
  const [who, setWho] = useState<number | undefined>(o?.invitable[0]?.id);
  if (!o) {
    return (
      <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span>
            {named && (
              <b>
                <Bar w="14ch" />
              </b>
            )}
            <span className="block font-mono text-xs">
              <Bar w="20ch" />
            </span>
          </span>
          <Pill>
            <Bar w="7ch" />
          </Pill>
        </div>
      </article>
    );
  }
  const [text, tone] = standing(say, o, day);

  return (
    <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          {named && (
            <Link to="/o/$org/companies/$id" params={{ org, id: o.company_id }} className="font-bold text-foreground hover:text-mark">
              {o.company}
            </Link>
          )}
          {o.name && (
            <span className="text-muted-foreground">
              {named ? joiner : ""}
              {o.name}
            </span>
          )}
          <span className="block font-mono text-xs text-muted-foreground">
            {say(v.progress, { done: o.done, total: o.steps.length, on: onDay(o.start, day) })}
          </span>
        </span>
        <Pill tone={tone}>{text}</Pill>
      </div>
      <div>
        {o.steps.map((s) => (
          <div key={s.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-2">
            <span className="min-w-0">
              <span className="font-mono text-xs text-muted-foreground">{say(v.who, { seq: s.seq, who: s.who })}</span>
              {s.done_on ? <span className="text-muted-foreground">{s.name}</span> : <b>{s.name}</b>}
              <span className="block font-mono text-xs text-muted-foreground">{stepMeta(say, s, day)}</span>
            </span>
            <span className="flex items-center gap-2">
              {support && !s.done_on && (
                <>
                  <Input
                    type="date"
                    defaultValue={s.due}
                    aria-label={say(m.due)}
                    className="h-8 w-44 bg-muted"
                    onChange={(e) => step.mutate({ params: { path: { org, id: s.id } }, body: { due: e.target.value } })}
                  />
                  <ActionButton size="sm" onClick={() => step.mutate({ params: { path: { org, id: s.id } }, body: { done: true } })}>
                    {say(m.done)}
                  </ActionButton>
                </>
              )}
              {support && s.done_on && (
                <ActionButton quiet size="xs" onClick={() => step.mutate({ params: { path: { org, id: s.id } }, body: { done: false } })}>
                  {say(m.notDone)}
                </ActionButton>
              )}
            </span>
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-2 border-t border-border pt-2">
        <Eyebrow>{say(m.theirPage)}</Eyebrow>
        <Meta>{say(m.theirPageWhat)}</Meta>
        {o.invites.map((inv) => (
          <div key={inv.id} className="flex flex-wrap items-center justify-between gap-2">
            <span>
              <b>{inv.name}</b>
              <span className="font-mono text-xs text-muted-foreground"> {inv.email}</span>
            </span>
            {inv.withdrawn ? (
              <Pill>{say(m.withdrawn)}</Pill>
            ) : (
              <span className="flex items-center gap-2">
                <Pill tone="ok">{say(m.canOpen)}</Pill>
                {support && (
                  <ActionButton
                    quiet
                    size="xs"
                    onClick={() => window.confirm(say(v.withdrawConfirm, { name: inv.name })) && withdraw.mutate({ params: { path: { org, id: inv.id } } })}
                  >
                    {say(m.withdraw)}
                  </ActionButton>
                )}
              </span>
            )}
          </div>
        ))}
        {!support ? (
          o.invites.length === 0 && <Meta>{say(m.noneInvited)}</Meta>
        ) : !o.can_invite ? (
          <Meta>{say(m.noMail)}</Meta>
        ) : o.invitable.length === 0 ? (
          <Meta>{say(m.noAddress)}</Meta>
        ) : (
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (who) invite.mutate({ params: { path: { org, id: o.id } }, body: { contact: who } });
            }}
          >
            <select
              aria-label={say(m.whoToInvite)}
              value={who}
              onChange={(e) => setWho(Number(e.target.value))}
              className="h-9 rounded-lg border border-input bg-muted px-2 text-sm"
            >
              {o.invitable.map((p) => (
                <option key={p.id} value={p.id}>
                  {say(v.invitee, { name: p.name, email: p.email })}
                </option>
              ))}
            </select>
            <ActionButton quiet size="sm" type="submit" disabled={invite.isPending}>
              {say(m.emailLink)}
            </ActionButton>
            {invite.isSuccess && <span className="font-mono text-xs text-muted-foreground">{say(m.sent)}</span>}
          </form>
        )}
      </div>
      {support && (
        <div className="flex justify-end">
          <ActionButton quiet size="xs" onClick={() => window.confirm(say(m.removeConfirm)) && remove.mutate({ params: { path: { org, id: o.id } } })}>
            {say(m.remove)}
          </ActionButton>
        </div>
      )}
      <Failed errors={[step.error, invite.error, withdraw.error, remove.error]} />
    </article>
  );
}
