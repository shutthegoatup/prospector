import { useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Failed, Meta, PageHead, useRefresh, Whose } from "@/components/app";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, plural } from "@/lib/format";
import { useSession } from "@/session";

type Campaign = components["schemas"]["Campaign"];

const audiences = [
  { key: "conversations", label: "Everyone in conversation" },
  { key: "customers", label: "Everyone at a customer" },
] as const;

const field = "rounded-lg border border-input bg-muted px-3";
const select = "h-9 rounded-lg border border-input bg-muted px-2 text-sm";

function kindLabel(kind: string) {
  return kind === "service" ? "Service notice" : "Marketing";
}

function audienceLabel(a: string) {
  return audiences.find((x) => x.key === a)?.label ?? a;
}

function mailLine(m: Record<string, number>) {
  const labels: [string, string][] = [
    ["sent", "sent"], ["queued", "waiting"], ["sending", "going now"], ["skipped", "skipped"],
    ["failed", "refused"], ["unknown", "unknown"], ["cancelled", "cancelled"],
  ];
  return labels.filter(([k]) => (m[k] ?? 0) > 0).map(([k, l]) => `${m[k]} ${l}`).join(" · ");
}

function state(c: Campaign) {
  switch (c.status) {
    case "sending":
      return "Sending";
    case "paused":
      return "Paused: " + c.paused;
    case "sent":
      return "Sent";
  }
  return "Cancelled";
}

export function CampaignsPage() {
  const { org, sales } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/campaigns", { params: { path: { org }, query: { limit: 200 } } });
  const refresh = useRefresh();
  const [name, setName] = useState("");
  const [kind, setKind] = useState("marketing");
  const create = $api.useMutation("post", "/o/{org}/api/v1/campaigns", {
    onSuccess: () => {
      setName("");
      refresh();
    },
  });
  const notReady = list.data?.not_ready;

  return (
    <>
      <PageHead title="Campaigns" />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        A marketing mail written once, for everyone in conversation or everyone at a company that is a customer now. Never for prospects who
        haven't replied: that stays one person writing to one person. Anyone on the list not to be mailed, on the never-contact list or erased
        is left out, and each draft shows who that leaves. It goes through your own mail server a few dozen a minute, each with your postal
        address and a link to unsubscribe, and nobody is mailed twice.
      </p>
      {notReady && (
        <p className="mb-5 max-w-[70ch] rounded-lg border border-border bg-muted px-3 py-2 text-sm">Not yet ready to send: {notReady}.</p>
      )}
      <Whose area="sales" work="Campaigns are" can={sales} />
      {sales && (
        <form
          className="mb-5 flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({ params: { path: { org } }, body: { name, kind } });
          }}
        >
          <Input
            required
            maxLength={120}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Spring price list"
            aria-label="Name of the campaign"
            className={`h-9 min-w-[220px] flex-1 text-sm ${field}`}
          />
          <select aria-label="What kind of mail" value={kind} onChange={(e) => setKind(e.target.value)} className={select}>
            <option value="marketing">Marketing</option>
            <option value="service">Service notice to customers</option>
          </select>
          <ActionButton type="submit" size="sm" disabled={create.isPending}>
            Start a draft
          </ActionButton>
          <Failed errors={[create.error]} />
        </form>
      )}
      {list.data && list.data.items.length === 0 && <Empty title="No campaigns yet" hint="Start a draft above, then see who it would go to." />}
      {list.data?.items.map((c) => (c.status === "draft" ? <DraftCard key={c.id} c={c} ready={!notReady} /> : <SentCard key={c.id} c={c} />))}
    </>
  );
}

function SentCard({ c }: { c: Campaign }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const resume = $api.useMutation("post", "/o/{org}/api/v1/campaigns/{id}/resume", { onSuccess: refresh });
  const cancel = $api.useMutation("post", "/o/{org}/api/v1/campaigns/{id}/cancel", { onSuccess: refresh });
  const path = { params: { path: { org, id: c.id } } };
  return (
    <article className="panel mb-4 flex flex-col gap-2 p-4 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <b>{c.name}</b>
        <span className="font-mono text-xs text-muted-foreground">{state(c)}</span>
      </div>
      <Meta>{[kindLabel(c.kind), audienceLabel(c.audience), "sent by " + actor(c.queued_by), mailLine(c.mail ?? {})].filter(Boolean).join(" · ")}</Meta>
      <p className="font-bold">{c.subject}</p>
      <p className="whitespace-pre-wrap text-muted-foreground">{c.body}</p>
      {(c.status === "sending" || c.status === "paused") && sales && (
        <div className="flex justify-end gap-2">
          {c.status === "paused" && (
            <ActionButton size="xs" onClick={() => resume.mutate(path)}>
              Resume
            </ActionButton>
          )}
          <ActionButton
            quiet
            size="xs"
            onClick={() => window.confirm(`Stop sending ${c.name}? What went stays sent.`) && cancel.mutate(path)}
          >
            Cancel the rest
          </ActionButton>
        </div>
      )}
      <Failed errors={[resume.error, cancel.error]} />
    </article>
  );
}

function DraftCard({ c, ready }: { c: Campaign; ready: boolean }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const audience = $api.useQuery("get", "/o/{org}/api/v1/campaigns/{id}/audience", { params: { path: { org, id: c.id }, query: { limit: 25 } } });
  const change = $api.useMutation("patch", "/o/{org}/api/v1/campaigns/{id}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/campaigns/{id}", { onSuccess: refresh });
  const send = $api.useMutation("post", "/o/{org}/api/v1/campaigns/{id}/send", { onSuccess: refresh });
  const path = { params: { path: { org, id: c.id } } };
  const save = (key: keyof components["schemas"]["CampaignChange"], value: string) => {
    if (value !== (c as Record<string, unknown>)[key]) change.mutate({ ...path, body: { [key]: value } });
  };
  const going = audience.data?.going ?? 0;
  const leftOut = audience.data?.left_out ?? 0;
  const shown = audience.data?.items ?? [];

  return (
    <article className="panel mb-4 flex flex-col gap-3 p-4 text-sm">
      {sales ? (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Input
              defaultValue={c.name}
              maxLength={120}
              aria-label="Name"
              onBlur={(e) => save("name", e.target.value)}
              className={`h-9 min-w-[220px] flex-1 font-bold ${field}`}
            />
            <select aria-label="What kind of mail" defaultValue={c.kind} onChange={(e) => save("kind", e.target.value)} className={select}>
              <option value="marketing">Marketing</option>
              <option value="service">Service notice</option>
            </select>
            {c.kind !== "service" && (
              <select aria-label="Who it is for" defaultValue={c.audience} onChange={(e) => save("audience", e.target.value)} className={select}>
                {audiences.map((a) => (
                  <option key={a.key} value={a.key}>
                    {a.label}
                  </option>
                ))}
              </select>
            )}
          </div>
          {c.kind === "service" && (
            <p className="rounded-lg border border-border bg-muted px-3 py-2">
              A service notice goes to everyone at a current customer, including anyone who unsubscribed from your marketing, because it is about
              what they already have: a price or terms change, an outage, a date that matters. Keep it to that. Anything promotional in it makes it
              marketing, whatever it is called. It never goes to anyone on the never-contact list or erased, and carries no unsubscribe link.
            </p>
          )}
          <Input
            defaultValue={c.subject}
            maxLength={200}
            placeholder="Subject"
            aria-label="Subject"
            onBlur={(e) => save("subject", e.target.value)}
            className={`h-9 ${field}`}
          />
          <Textarea
            rows={8}
            maxLength={20000}
            defaultValue={c.body}
            placeholder="What it says, as plain text. Your postal address is added at the foot, and for marketing an unsubscribe link."
            aria-label="What it says"
            onBlur={(e) => save("body", e.target.value)}
            className={`py-2 leading-relaxed ${field}`}
          />
        </>
      ) : (
        <>
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <b className="text-base">{c.name}</b>
            <Meta>
              {kindLabel(c.kind)} · {audienceLabel(c.audience)}
            </Meta>
          </div>
          <p className={c.subject ? "" : "text-muted-foreground"}>{c.subject || "No subject yet"}</p>
          {c.body && <p className="whitespace-pre-wrap rounded-lg border-l-4 border-primary bg-muted px-3 py-2">{c.body}</p>}
        </>
      )}
      <details>
        <summary className="cursor-pointer font-mono text-xs text-muted-foreground">
          Would go to {plural(going, "person", "people")}; {leftOut} left out
        </summary>
        <div className="mt-2 flex flex-col">
          {shown.map((p) => (
            <div key={p.contact_id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-1.5">
              <span>
                <b>{p.name}</b> <span className="text-muted-foreground">{[p.company, p.address].filter(Boolean).join(" · ")}</span>
              </span>
              {p.left_out && <span className="font-mono text-xs text-muted-foreground">left out: {p.left_out}</span>}
            </div>
          ))}
          {going + leftOut > shown.length && <Meta>The first {shown.length} are shown. The API lists them all.</Meta>}
        </div>
      </details>
      {sales && (
        <div className="flex justify-end gap-2">
          <ActionButton quiet size="xs" onClick={() => window.confirm(`Remove the draft ${c.name}?`) && remove.mutate(path)}>
            Remove draft
          </ActionButton>
          {ready && going > 0 && c.subject && c.body && (
            <ActionButton
              size="xs"
              onClick={() => window.confirm(`Send ${c.name} to ${plural(going, "person", "people")} now?`) && send.mutate(path)}
            >
              Send to {plural(going, "person", "people")}
            </ActionButton>
          )}
        </div>
      )}
      <Failed errors={[change.error, remove.error, send.error]} />
    </article>
  );
}
