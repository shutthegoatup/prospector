import { useState } from "react";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Failed, Meta, PageHead, useRefresh, Whose } from "@/components/app";
import { Bar, Loading, Text } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { intl } from "@/i18n";
import { actor } from "@/lib/format";
import { useSession } from "@/session";

type Campaign = components["schemas"]["Campaign"];
type Audience = components["schemas"]["CampaignAudience"];

const m = defineMessages({
  title: { defaultMessage: "Campaigns", id: "QQ/laW", description: "Title of the page of mail campaigns" },
  intro: {
    defaultMessage:
      "A marketing mail written once, for everyone in conversation or everyone at a company that is a customer now. Never for prospects who haven't replied: that stays one person writing to one person. Anyone on the list not to be mailed, on the never-contact list or erased is left out, and each draft shows who that leaves. It goes through your own mail server a few dozen a minute, each with your postal address and a link to unsubscribe, and nobody is mailed twice.",
    id: "TcCW6A",
    description: "Introduction at the top of the Campaigns page",
  },
  whose: {
    defaultMessage: "Campaigns are sales work, so here you read it.",
    id: "B5f1u5",
    description: "On the Campaigns page, to a member without sales work: why they can read it but not act",
  },
  namePlaceholder: { defaultMessage: "e.g. Spring price list", id: "TwURc5", description: "Example in the empty box for a new campaign's name" },
  nameOfCampaign: { defaultMessage: "Name of the campaign", id: "v7TFRo", description: "What a screen reader calls the box for a new campaign's name" },
  whatKind: {
    defaultMessage: "What kind of mail",
    id: "7tglVP",
    description: "What a screen reader calls the choice between a marketing mail and a service notice",
  },
  serviceToCustomers: {
    defaultMessage: "Service notice to customers",
    id: "5BKDBR",
    description: "Choice of kind for a new campaign: a notice to customers about what they already have",
  },
  startDraft: { defaultMessage: "Start a draft", id: "jXBRhM", description: "Button creating a new campaign as a draft" },
  noCampaigns: { defaultMessage: "No campaigns yet", id: "2p7s9N", description: "Title when an organisation has no campaigns" },
  noCampaignsHint: {
    defaultMessage: "Start a draft above, then see who it would go to.",
    id: "/Tjak5",
    description: "Hint when an organisation has no campaigns",
  },
  resume: { defaultMessage: "Resume", id: "CKD81Q", description: "Button starting a paused campaign sending again" },
  cancelRest: { defaultMessage: "Cancel the rest", id: "9hzC6X", description: "Button stopping a campaign's mails not yet sent" },
  name: { defaultMessage: "Name", id: "2Gc4ge", description: "What a screen reader calls the box with a draft campaign's name" },
  whoFor: { defaultMessage: "Who it is for", id: "5nCYQu", description: "What a screen reader calls the choice of whom a marketing campaign goes to" },
  serviceNote: {
    defaultMessage:
      "A service notice goes to everyone at a current customer, including anyone who unsubscribed from your marketing, because it is about what they already have: a price or terms change, an outage, a date that matters. Keep it to that. Anything promotional in it makes it marketing, whatever it is called. It never goes to anyone on the never-contact list or erased, and carries no unsubscribe link.",
    id: "CDQhq2",
    description: "On a draft campaign that is a service notice: what one is for and whom it reaches",
  },
  subject: { defaultMessage: "Subject", id: "XzzEGx", description: "Placeholder and screen reader name of the box with a campaign mail's subject" },
  bodyPlaceholder: {
    defaultMessage: "What it says, as plain text. Your postal address is added at the foot, and for marketing an unsubscribe link.",
    id: "3qEhNr",
    description: "Placeholder in the empty box for a campaign mail's text",
  },
  body: { defaultMessage: "What it says", id: "Wc2DxA", description: "What a screen reader calls the box with a campaign mail's text" },
  noSubject: { defaultMessage: "No subject yet", id: "YLZcVa", description: "In place of a draft campaign's subject when it has none" },
  removeDraft: { defaultMessage: "Remove draft", id: "2GslWn", description: "Button deleting a draft campaign" },
});

const v = {
  notReady: defineMessage<{ reason: string }>({
    defaultMessage: "Not yet ready to send: {reason}.",
    id: "zH3KfT",
    description: "On the Campaigns page when nothing can be sent yet. {reason} is the server's reason, such as a postal address missing",
  }),
  paused: defineMessage<{ reason: string }>({
    defaultMessage: "Paused: {reason}",
    id: "yla9ZH",
    description: "A campaign's state when sending stopped by itself. {reason} is the server's reason",
  }),
  sentBy: defineMessage<{ who: string }>({
    defaultMessage: "sent by {who}",
    id: "DSMaR+",
    description: "In the line under a sent campaign's name, between its kind and its mail counts. {who} is a member or an agent",
  }),
  stopSending: defineMessage<{ name: string }>({
    defaultMessage: "Stop sending {name}? What went stays sent.",
    id: "cWgNpe",
    description: "Asked before cancelling a campaign's mails not yet sent. {name} is the campaign's name",
  }),
  removeDraft: defineMessage<{ name: string }>({
    defaultMessage: "Remove the draft {name}?",
    id: "qgxe5Q",
    description: "Asked before deleting a draft campaign. {name} is the campaign's name",
  }),
  people: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# person} other {# people}}",
    id: "m7FgGD",
    description: "How many a draft campaign would go to, shown after Would go to",
  }),
  leftOut: defineMessage<{ reason: string }>({
    defaultMessage: "left out: {reason}",
    id: "QCX+/9",
    description: "Beside someone a draft campaign would not go to. {reason} is the server's reason, such as no email address",
  }),
  firstShown: defineMessage<{ n: number }>({
    defaultMessage: "The first {n} are shown. The API lists them all.",
    id: "OcHfxi",
    description: "Under a draft campaign's list of recipients when only some are shown",
  }),
  sendNow: defineMessage<{ name: string; n: number }>({
    defaultMessage: "Send {name} to {n, plural, one {# person} other {# people}} now?",
    id: "q9MVmm",
    description: "Asked before sending a campaign. {name} is the campaign's name",
  }),
  sendTo: defineMessage<{ n: number }>({
    defaultMessage: "Send to {n, plural, one {# person} other {# people}}",
    id: "2VEkh4",
    description: "Button sending a draft campaign to everyone it would go to",
  }),
};

const kinds = defineMessages({
  marketing: { defaultMessage: "Marketing", id: "5AGMji", description: "A campaign's kind: a marketing mail" },
  service: { defaultMessage: "Service notice", id: "erqsBj", description: "A campaign's kind: a notice to customers about what they already have" },
});

const audienceNames = defineMessages({
  conversations: {
    defaultMessage: "Everyone in conversation",
    id: "OwBy3m",
    description: "Whom a campaign goes to: everyone in a conversation with the organisation",
  },
  customers: { defaultMessage: "Everyone at a customer", id: "UuJ//c", description: "Whom a campaign goes to: everyone at a company that is a customer" },
});

const audiences = Object.keys(audienceNames) as Audience[];

const states = defineMessages({
  sending: { defaultMessage: "Sending", id: "Tjl+Ta", description: "A campaign's state: its mails are going out" },
  sent: { defaultMessage: "Sent", id: "HvA02s", description: "A campaign's state: every mail has been dealt with" },
  cancelled: { defaultMessage: "Cancelled", id: "SinXwo", description: "A campaign's state: someone stopped it" },
});

// In the order a sent campaign's mail counts are listed.
const mailCounts = {
  sent: defineMessage<{ n: number }>({ defaultMessage: "{n} sent", id: "Vjg0KF", description: "Count of a campaign's mails sent, in the line under its name" }),
  queued: defineMessage<{ n: number }>({
    defaultMessage: "{n} waiting",
    id: "TuHm1d",
    description: "Count of a campaign's mails waiting to go, in the line under its name",
  }),
  sending: defineMessage<{ n: number }>({
    defaultMessage: "{n} going now",
    id: "z23zX3",
    description: "Count of a campaign's mails going out now, in the line under its name",
  }),
  skipped: defineMessage<{ n: number }>({
    defaultMessage: "{n} skipped",
    id: "ehKP45",
    description: "Count of a campaign's mails skipped, in the line under its name",
  }),
  failed: defineMessage<{ n: number }>({
    defaultMessage: "{n} refused",
    id: "dGSIwS",
    description: "Count of a campaign's mails the mail server refused, in the line under its name",
  }),
  unknown: defineMessage<{ n: number }>({
    defaultMessage: "{n} unknown",
    id: "U9jxGD",
    description: "Count of a campaign's mails whose fate is unknown, in the line under its name",
  }),
  cancelled: defineMessage<{ n: number }>({
    defaultMessage: "{n} cancelled",
    id: "6dTQKZ",
    description: "Count of a campaign's mails cancelled, in the line under its name",
  }),
};

const dot = " · ";

const field = "rounded-lg border border-input bg-muted px-3";
const select = "h-9 rounded-lg border border-input bg-muted px-2 text-sm";

function kindLabel(kind: string) {
  return intl.formatMessage(kind === "service" ? kinds.service : kinds.marketing);
}

function audienceLabel(a: Audience) {
  return intl.formatMessage(audienceNames[a]);
}

function mailLine(counts: Record<string, number>) {
  return (Object.keys(mailCounts) as (keyof typeof mailCounts)[])
    .filter((k) => (counts[k] ?? 0) > 0)
    .map((k) => intl.formatMessage(mailCounts[k], { n: counts[k] ?? 0 }))
    .join(dot);
}

function state(c: Campaign) {
  switch (c.status) {
    case "sending":
    case "sent":
      return intl.formatMessage(states[c.status]);
    case "paused":
      return intl.formatMessage(v.paused, { reason: c.paused });
  }
  return intl.formatMessage(states.cancelled);
}

export function CampaignsPage() {
  const say = useIntl().formatMessage;
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
      <PageHead title={say(m.title)} />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">{say(m.intro)}</p>
      {/* Whether it can send yet comes with the list and sits above everything else, so
          nothing below the introduction is drawn until then, rather than drawn and pushed down. */}
      {!list.data && !list.error && <Loading busy>{null}</Loading>}
      {(list.data || list.error) && (
        <>
          {list.error && (
            <div className="mb-5">
              <Failed errors={[list.error]} />
            </div>
          )}
          {notReady && <p className="mb-5 max-w-[70ch] rounded-lg border border-border bg-muted px-3 py-2 text-sm">{say(v.notReady, { reason: notReady })}</p>}
          <Whose area="sales" said={say(m.whose)} can={sales} />
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
                placeholder={say(m.namePlaceholder)}
                aria-label={say(m.nameOfCampaign)}
                className={`h-9 min-w-[220px] flex-1 text-sm ${field}`}
              />
              <select aria-label={say(m.whatKind)} value={kind} onChange={(e) => setKind(e.target.value)} className={select}>
                <option value="marketing">{say(kinds.marketing)}</option>
                <option value="service">{say(m.serviceToCustomers)}</option>
              </select>
              <ActionButton type="submit" size="sm" disabled={create.isPending}>
                {say(m.startDraft)}
              </ActionButton>
              <Failed errors={[create.error]} />
            </form>
          )}
          {list.data?.items.length === 0 && <Empty title={say(m.noCampaigns)} hint={say(m.noCampaignsHint)} />}
          {list.data?.items.map((c) => (c.status === "draft" ? <DraftCard key={c.id} c={c} ready={!notReady} /> : <SentCard key={c.id} c={c} />))}
        </>
      )}
    </>
  );
}

// SentCard is a campaign that has gone out, or a campaign's outline while the list loads.
function SentCard({ c }: { c?: Campaign }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const resume = $api.useMutation("post", "/o/{org}/api/v1/campaigns/{id}/resume", { onSuccess: refresh });
  const cancel = $api.useMutation("post", "/o/{org}/api/v1/campaigns/{id}/cancel", { onSuccess: refresh });
  if (!c) {
    return (
      <article className="panel mb-4 flex flex-col gap-2 p-4 text-sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <b>
            <Bar w="16ch" />
          </b>
          <span className="font-mono text-xs">
            <Bar w="5ch" />
          </span>
        </div>
        <Meta>
          <Bar w="36ch" />
        </Meta>
        <p className="font-bold">
          <Bar w="24ch" />
        </p>
      </article>
    );
  }
  const path = { params: { path: { org, id: c.id } } };
  return (
    <article className="panel mb-4 flex flex-col gap-2 p-4 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <b>{c.name}</b>
        <span className="font-mono text-xs text-muted-foreground">{state(c)}</span>
      </div>
      <Meta>
        {[kindLabel(c.kind), audienceLabel(c.audience), say(v.sentBy, { who: actor(c.queued_by) }), mailLine(c.mail ?? {})].filter(Boolean).join(dot)}
      </Meta>
      <p className="font-bold">{c.subject}</p>
      <p className="whitespace-pre-wrap text-muted-foreground">{c.body}</p>
      {(c.status === "sending" || c.status === "paused") && sales && (
        <div className="flex justify-end gap-2">
          {c.status === "paused" && (
            <ActionButton size="xs" onClick={() => resume.mutate(path)}>
              {say(m.resume)}
            </ActionButton>
          )}
          <ActionButton quiet size="xs" onClick={() => window.confirm(say(v.stopSending, { name: c.name })) && cancel.mutate(path)}>
            {say(m.cancelRest)}
          </ActionButton>
        </div>
      )}
      <Failed errors={[resume.error, cancel.error]} />
    </article>
  );
}

function DraftCard({ c, ready }: { c: Campaign; ready: boolean }) {
  const say = useIntl().formatMessage;
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
  const counted = audience.data !== undefined || Boolean(audience.error);

  return (
    <article className="panel mb-4 flex flex-col gap-3 p-4 text-sm">
      {sales ? (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Input
              defaultValue={c.name}
              maxLength={120}
              aria-label={say(m.name)}
              onBlur={(e) => save("name", e.target.value)}
              className={`h-9 min-w-[220px] flex-1 font-bold ${field}`}
            />
            <select aria-label={say(m.whatKind)} defaultValue={c.kind} onChange={(e) => save("kind", e.target.value)} className={select}>
              <option value="marketing">{say(kinds.marketing)}</option>
              <option value="service">{say(kinds.service)}</option>
            </select>
            {c.kind !== "service" && (
              <select aria-label={say(m.whoFor)} defaultValue={c.audience} onChange={(e) => save("audience", e.target.value)} className={select}>
                {audiences.map((a) => (
                  <option key={a} value={a}>
                    {say(audienceNames[a])}
                  </option>
                ))}
              </select>
            )}
          </div>
          {c.kind === "service" && <p className="rounded-lg border border-border bg-muted px-3 py-2">{say(m.serviceNote)}</p>}
          <Input
            defaultValue={c.subject}
            maxLength={200}
            placeholder={say(m.subject)}
            aria-label={say(m.subject)}
            onBlur={(e) => save("subject", e.target.value)}
            className={`h-9 ${field}`}
          />
          <Textarea
            rows={8}
            maxLength={20000}
            defaultValue={c.body}
            placeholder={say(m.bodyPlaceholder)}
            aria-label={say(m.body)}
            onBlur={(e) => save("body", e.target.value)}
            className={`py-2 leading-relaxed ${field}`}
          />
        </>
      ) : (
        <>
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <b className="text-base">{c.name}</b>
            <Meta>{[kindLabel(c.kind), audienceLabel(c.audience)].join(dot)}</Meta>
          </div>
          <p className={c.subject ? "" : "text-muted-foreground"}>{c.subject || say(m.noSubject)}</p>
          {c.body && <p className="whitespace-pre-wrap rounded-lg border-l-4 border-primary bg-muted px-3 py-2">{c.body}</p>}
        </>
      )}
      <details>
        <summary className="cursor-pointer font-mono text-xs text-muted-foreground">
          <FormattedMessage
            defaultMessage="Would go to {people}; {leftOut} left out"
            id="94B7zC"
            description="Opens a draft campaign's recipients. {people} is how many it would go to, such as 3 people; {leftOut} is how many it leaves out"
            values={{
              people: <Text value={counted ? say(v.people, { n: going }) : undefined} w="9ch" />,
              leftOut: <Text value={counted ? intl.formatNumber(leftOut) : undefined} w="2ch" />,
            }}
          />
        </summary>
        <div className="mt-2 flex flex-col">
          {shown.map((p) => (
            <div key={p.contact_id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-1.5">
              <span>
                <b>{p.name}</b> <span className="text-muted-foreground">{[p.company, p.address].filter(Boolean).join(dot)}</span>
              </span>
              {p.left_out && <span className="font-mono text-xs text-muted-foreground">{say(v.leftOut, { reason: p.left_out })}</span>}
            </div>
          ))}
          {going + leftOut > shown.length && <Meta>{say(v.firstShown, { n: shown.length })}</Meta>}
        </div>
      </details>
      {sales && (
        <div className="flex justify-end gap-2">
          <ActionButton quiet size="xs" onClick={() => window.confirm(say(v.removeDraft, { name: c.name })) && remove.mutate(path)}>
            {say(m.removeDraft)}
          </ActionButton>
          {ready && going > 0 && c.subject && c.body && (
            <ActionButton size="xs" onClick={() => window.confirm(say(v.sendNow, { name: c.name, n: going })) && send.mutate(path)}>
              {say(v.sendTo, { n: going })}
            </ActionButton>
          )}
        </div>
      )}
      <Failed errors={[audience.error, change.error, remove.error, send.error]} />
    </article>
  );
}
