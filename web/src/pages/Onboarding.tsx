import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, Meta, PageHead, Pill, type Tone, useRefresh, Whose } from "@/components/app";
import { Bar, Placeholders, placeholders } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { actor, plural } from "@/lib/format";
import { useSession } from "@/session";

type Onboarding = components["schemas"]["Onboarding"];
type Step = components["schemas"]["OnboardingStep"];

export function today() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function standing(o: Onboarding, day: string): [string, Tone] {
  const dueToday = o.steps.filter((s) => !s.done_on && s.due === day).length;
  if (o.live_on) return ["live since " + o.live_on, "ok"];
  if (o.late > 0) return [plural(o.late, "step") + " late", "stop"];
  if (dueToday > 0) return [plural(dueToday, "step") + " due today", "warn"];
  return ["on track", "agent"];
}

function stepMeta(s: Step, day: string) {
  if (s.done_on) return ["done " + s.done_on, actor(s.done_by)].filter(Boolean).join(" · ");
  if (s.due && s.due < day) return "late, was due " + s.due;
  if (s.due) return "due " + s.due;
  return "no date";
}

export function OnboardingPage() {
  const { org, support } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/onboardings", { params: { path: { org }, query: { limit: 200 } } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const planned = (settings.data?.onboarding_steps ?? "").split("\n").filter((l) => l.trim()).length;
  const day = today();
  // The cards wait for the settings too, as "No steps written yet" would go above them.
  const loading = (!list.data || settings.isPending) && !list.error && !settings.error;
  return (
    <>
      <PageHead title="Onboarding" />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        The steps between a signature and a live customer. Each onboarding is a copy of your steps, made the day it starts, so changing them in Settings later
        leaves the ones running as they are. Start one from a company's page. Ones running late come first.
      </p>
      <Whose area="support" work="Onboarding is" can={support} />
      {!loading && settings.data && planned === 0 && (
        <Empty title="No steps written yet" hint="An owner writes the steps under Settings, one a line. Then start an onboarding from a company's page." />
      )}
      {!loading && settings.data && planned > 0 && list.data && list.data.items.length === 0 && (
        <Empty title="Nobody is being onboarded" hint="Open a company and start one." />
      )}
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
  const [text, tone] = standing(o, day);

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
              {named ? " · " : ""}
              {o.name}
            </span>
          )}
          <span className="block font-mono text-xs text-muted-foreground">
            {o.done} of {o.steps.length} · started {o.start}
          </span>
        </span>
        <Pill tone={tone}>{text}</Pill>
      </div>
      <div>
        {o.steps.map((s) => (
          <div key={s.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-2">
            <span className="min-w-0">
              <span className="font-mono text-xs text-muted-foreground">
                {s.seq} · {s.who === "them" ? "them" : "us"} ·{" "}
              </span>
              {s.done_on ? <span className="text-muted-foreground">{s.name}</span> : <b>{s.name}</b>}
              <span className="block font-mono text-xs text-muted-foreground">{stepMeta(s, day)}</span>
            </span>
            <span className="flex items-center gap-2">
              {support && !s.done_on && (
                <>
                  <Input
                    type="date"
                    defaultValue={s.due}
                    aria-label="Due"
                    className="h-8 w-44 bg-muted"
                    onChange={(e) => step.mutate({ params: { path: { org, id: s.id } }, body: { due: e.target.value } })}
                  />
                  <ActionButton size="sm" onClick={() => step.mutate({ params: { path: { org, id: s.id } }, body: { done: true } })}>
                    Done
                  </ActionButton>
                </>
              )}
              {support && s.done_on && (
                <ActionButton quiet size="xs" onClick={() => step.mutate({ params: { path: { org, id: s.id } }, body: { done: false } })}>
                  Not done
                </ActionButton>
              )}
            </span>
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-2 border-t border-border pt-2">
        <Eyebrow>Their page</Eyebrow>
        <Meta>
          Someone you invite is emailed a link to a page showing the steps that are theirs, which they can mark done, and nothing else. It needs no password and
          opens in one browser at a time.
        </Meta>
        {o.invites.map((inv) => (
          <div key={inv.id} className="flex flex-wrap items-center justify-between gap-2">
            <span>
              <b>{inv.name}</b>
              <span className="font-mono text-xs text-muted-foreground"> {inv.email}</span>
            </span>
            {inv.withdrawn ? (
              <Pill>withdrawn</Pill>
            ) : (
              <span className="flex items-center gap-2">
                <Pill tone="ok">can open it</Pill>
                {support && (
                  <ActionButton
                    quiet
                    size="xs"
                    onClick={() =>
                      window.confirm(`Withdraw ${inv.name}'s access? Their page closes in every browser at once.`) &&
                      withdraw.mutate({ params: { path: { org, id: inv.id } } })
                    }
                  >
                    Withdraw
                  </ActionButton>
                )}
              </span>
            )}
          </div>
        ))}
        {!support ? (
          o.invites.length === 0 && <Meta>Nobody has been invited yet.</Meta>
        ) : !o.can_invite ? (
          <Meta>To invite anyone, an owner sets a mail server in Settings and the server needs its public address (PROSPECTOR_URL).</Meta>
        ) : o.invitable.length === 0 ? (
          <Meta>Nobody at this company has an email address here to send a link to.</Meta>
        ) : (
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (who) invite.mutate({ params: { path: { org, id: o.id } }, body: { contact: who } });
            }}
          >
            <select
              aria-label="Who to invite"
              value={who}
              onChange={(e) => setWho(Number(e.target.value))}
              className="h-9 rounded-lg border border-input bg-muted px-2 text-sm"
            >
              {o.invitable.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.email}
                </option>
              ))}
            </select>
            <ActionButton quiet size="sm" type="submit" disabled={invite.isPending}>
              Email them a link
            </ActionButton>
            {invite.isSuccess && <span className="font-mono text-xs text-muted-foreground">Sent.</span>}
          </form>
        )}
      </div>
      {support && (
        <div className="flex justify-end">
          <ActionButton
            quiet
            size="xs"
            onClick={() =>
              window.confirm("Remove this onboarding and its steps? Use this for one started by mistake.") &&
              remove.mutate({ params: { path: { org, id: o.id } } })
            }
          >
            Remove
          </ActionButton>
        </div>
      )}
      <Failed errors={[step.error, invite.error, withdraw.error, remove.error]} />
    </article>
  );
}
