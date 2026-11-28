import { type ReactNode, useState } from "react";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, Section, useRefresh } from "@/components/app";
import { Bar, Placeholders, Text } from "@/components/app/skeleton";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { writeClipboard } from "@/lib/clipboard";
import { stamp } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useSession } from "@/session";

type Key = components["schemas"]["APIKey"];
type Endpoint = components["schemas"]["WebhookEndpoint"];
type EventType = components["schemas"]["WebhookEventType"];
type Delivery = components["schemas"]["WebhookDelivery"];

const prospector = "Prospector";
const fieldClass = "h-10 rounded-lg border border-input bg-muted px-3 text-sm disabled:opacity-60";
const cardClass = "rail rounded-[14px] border-2 border-ink bg-card";
const exampleURL = "https://crm.example.com/hooks/prospector";
const standardWebhooks = "https://www.standardwebhooks.com/";
const deliveriesShown = 10;
const bodiless = { "Content-Type": "application/json" };

const standardLink = (c: ReactNode[]) => (
  <a className="underline" href={standardWebhooks} target="_blank" rel="noopener">
    {c}
  </a>
);

const m = defineMessages({
  title: { defaultMessage: "Webhooks", id: "/Ui2kK", description: "Heading of the agents page's section of endpoints the organisation's events are posted to" },
  none: { defaultMessage: "Nothing is sent anywhere yet.", id: "hyPlCy", description: "In the Webhooks section when no endpoint is set up" },
  sent: { defaultMessage: "Sent", id: "7YlTbn", description: "Label before the events an endpoint is sent" },
  change: { defaultMessage: "Change", id: "i1YhL/", description: "On an endpoint's card: opens what can be changed about it" },
  whatItIsSent: { defaultMessage: "What it is sent", id: "7Pd4MO", description: "Label above the events an endpoint can be sent, as checkboxes" },
  save: { defaultMessage: "Save", id: "QXeZss", description: "Button saving which events an endpoint is sent" },
  saved: { defaultMessage: "Saved ✓", id: "LARsVT", description: "After an endpoint's events are saved" },
  newSecret: { defaultMessage: "New secret", id: "BhEOIe", description: "Button giving an endpoint a new signing secret" },
  stop: { defaultMessage: "Stop", id: "miItOE", description: "Button to stop sending to an endpoint" },
  sendAgain: { defaultMessage: "Send again", id: "malLjX", description: "Button to start sending to an endpoint that was stopped" },
  remove: { defaultMessage: "Remove", id: "VrB7gL", description: "Button removing an endpoint" },
  lastSent: { defaultMessage: "Last sent", id: "eHMToe", description: "Label above the list of an endpoint's latest deliveries" },
  nothingSent: { defaultMessage: "Nothing sent yet.", id: "u6gwwT", description: "In an endpoint's deliveries when none has been sent" },
  when: { defaultMessage: "When", id: "5IW/kP", description: "Column heading: when a delivery was made" },
  event: { defaultMessage: "Event", id: "npap9+", description: "Column heading: the kind of event delivered" },
  howItWent: { defaultMessage: "How it went", id: "naKg6T", description: "Column heading: whether a delivery was taken" },
  tries: { defaultMessage: "Tries", id: "UoZFGX", description: "Column heading: how many times a delivery was tried" },
  cancelled: { defaultMessage: "not sent", id: "BdH+qr", description: "A delivery dropped because its endpoint was stopped or its key revoked" },
  addEndpoint: { defaultMessage: "Add an endpoint", id: "s7QSdk", description: "Heading of the form adding an endpoint" },
  address: { defaultMessage: "Address", id: "6ZliXC", description: "Label of an endpoint's https address" },
  addressHint: {
    defaultMessage: "https, on the internet: nothing is posted to a private address.",
    id: "iHXNmZ",
    description: "Under the endpoint address field",
  },
  key: { defaultMessage: "Key", id: "mYChyH", description: "Label of the API key an endpoint is sent events as" },
  keyHint: {
    defaultMessage: "It is sent only what this key may read, and nothing once the key is revoked.",
    id: "hwzOfH",
    description: "Under the endpoint's key choice",
  },
  noReadingKey: {
    defaultMessage: "Make a key that can read first, under Who works here.",
    id: "UePXLu",
    description: "Instead of the key choice when no key has the read scope; Who works here is a section of the page",
  },
  events: { defaultMessage: "Events", id: "Bco8ow", description: "Label above the events a new endpoint is sent" },
  add: { defaultMessage: "Add", id: "fya6ll", description: "Button adding an endpoint" },
  shownOnce: { defaultMessage: "Its signing secret, shown only now:", id: "O9LCAi", description: "Above an endpoint's new secret" },
  copy: { defaultMessage: "Copy", id: "jU4UrH", description: "Button copying an endpoint's secret" },
  copied: { defaultMessage: "Copied ✓", id: "/2ROzN", description: "The copy button once it has copied the secret" },
  lostIt: {
    defaultMessage: "If you lose it, give the endpoint a new secret: for a day, posts are signed with both.",
    id: "JjrCrY",
    description: "Under an endpoint's secret",
  },
  secret: { defaultMessage: "Signing secret", id: "zoMTdr", description: "What a screen reader calls an endpoint's secret" },
});

const v = {
  hint: defineMessage<{ prospector: string }>({
    defaultMessage:
      "{prospector} posts to your systems as things happen: a company added, a person marked, a deal changed. Each endpoint uses one of your keys and is sent only what that key may read: ids and links, which your system reads back with the key.",
    id: "wTSsG4",
    description: "Under the heading of the Webhooks section. {prospector} is the product's name",
  }),
  sending: defineMessage<{ key: string }>({
    defaultMessage: "sending · with the key {key}",
    id: "O2eNSs",
    description: "Under an endpoint that is being sent to. {key} is the name of its API key",
  }),
  stopped: defineMessage<{ when: string; why: string; key: string }>({
    defaultMessage: "stopped {when} · {why} · with the key {key}",
    id: "9wIDCS",
    description: "Under an endpoint that was stopped. {when} is when, {why} why, {key} its API key's name",
  }),
  taken: defineMessage<{ code: number }>({
    defaultMessage: "taken · {code}",
    id: "4JILpW",
    description: "A delivery the endpoint took. {code} is the HTTP status",
  }),
  failed: defineMessage<{ code: number }>({
    defaultMessage: "given up · {code}",
    id: "lgcH5X",
    description: "A delivery given up for good. {code} is the HTTP status, 0 if unreachable",
  }),
  retrying: defineMessage<{ when: string; code: number }>({
    defaultMessage: "trying again {when} · {code}",
    id: "bhywpy",
    description: "A delivery that failed and will be tried again. {when} is when, {code} the HTTP status",
  }),
  removeAsk: defineMessage<{ url: string }>({
    defaultMessage: "Stop sending to {url} for good?",
    id: "npIEEw",
    description: "Asked before removing an endpoint. {url} is its address",
  }),
  newSecretAsk: defineMessage<{ url: string }>({
    defaultMessage: "Give {url} a new secret? The old one keeps signing for a day.",
    id: "oyyljK",
    description: "Asked before giving an endpoint a new secret. {url} is its address",
  }),
};

// Webhooks are where an owner sends the organisation's events, each through one of its keys.
export function WebhooksSection({ keys }: { keys?: Key[] }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/webhooks", { params: { path: { org }, query: { limit: 200 } } });
  const kinds = $api.useQuery("get", "/o/{org}/api/v1/webhook-events", { params: { path: { org }, query: { limit: 200 } } });
  const [secret, setSecret] = useState<string>();
  const endpoints = list.data?.items;
  return (
    <Section title={say(m.title)} hint={say(v.hint, { prospector })}>
      {!endpoints && !list.error && (
        <Placeholders>
          <EndpointCard />
        </Placeholders>
      )}
      {endpoints?.length === 0 && <p className="text-sm">{say(m.none)}</p>}
      {endpoints?.map((e) => (
        <EndpointCard key={e.id} e={e} kinds={kinds.data?.items ?? []} onSecret={setSecret} />
      ))}
      {secret && <Secret secret={secret} />}
      {kinds.data && keys && <AddEndpoint keys={keys} kinds={kinds.data.items} onSecret={setSecret} />}
      <Failed errors={[list.error, kinds.error]} />
    </Section>
  );
}

function Secret({ secret }: { secret: string }) {
  const say = useIntl().formatMessage;
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-3 rounded-lg border-2 border-ink bg-muted p-4" role="status">
      <p className="text-sm">{say(m.shownOnce)}</p>
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-input bg-card px-3 py-2">
        <code aria-label={say(m.secret)} className="min-w-0 flex-1 break-all font-mono text-xs">
          {secret}
        </code>
        <ActionButton
          quiet
          size="xs"
          onClick={() => {
            writeClipboard(secret);
            setCopied(true);
          }}
        >
          {copied ? say(m.copied) : say(m.copy)}
        </ActionButton>
      </div>
      <p className="text-sm">
        <FormattedMessage
          defaultMessage="Check each post with it: deliveries are signed as <link>Standard Webhooks</link> sign them, which has a library for most languages."
          id="tvsPRQ"
          description="After an endpoint is added. <link> goes to the Standard Webhooks site, a name"
          values={{ link: standardLink }}
        />
      </p>
      <Meta>{say(m.lostIt)}</Meta>
    </div>
  );
}

function EventBoxes({ kinds, ticked, onChange, about }: { kinds: EventType[]; ticked: string[]; onChange: (ticked: string[]) => void; about?: boolean }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(12.5rem,1fr))] gap-x-3.5 gap-y-1">
      {kinds.map((k) => (
        <label key={k.name} className="flex items-start gap-2 text-sm">
          <Checkbox
            className="mt-1"
            checked={ticked.includes(k.name)}
            onCheckedChange={(on) => onChange(on ? [...ticked, k.name] : ticked.filter((n) => n !== k.name))}
          />
          <span>
            <code className="font-mono text-xs text-mark">{k.name}</code>
            {about && <span className="block text-muted-foreground">{k.about}</span>}
          </span>
        </label>
      ))}
    </div>
  );
}

function EndpointCard({ e, kinds = [], onSecret }: { e?: Endpoint; kinds?: EventType[]; onSecret?: (s: string) => void }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const [open, setOpen] = useState(false);
  const [ticked, setTicked] = useState(e?.events ?? []);
  const path = { org, id: e?.id ?? 0 };
  const change = $api.useMutation("patch", "/o/{org}/api/v1/webhooks/{id}", { onSuccess: refresh });
  const drop = $api.useMutation("delete", "/o/{org}/api/v1/webhooks/{id}", { onSuccess: refresh });
  const rotate = $api.useMutation("post", "/o/{org}/api/v1/webhooks/{id}/secret", { onSuccess: (d) => onSecret?.(d.secret) });
  const stopped = !!e?.disabled_at;
  return (
    <details className={cardClass} data-state={e && !stopped ? "ok" : ""} onToggle={(ev) => setOpen(ev.currentTarget.open)}>
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="break-all font-heading text-[15px] font-bold">
              <Text value={e?.url} w="28ch" />
            </span>
            <span className={cn("font-mono text-xs", stopped ? "text-[var(--stop)]" : "text-[var(--ok)]")}>
              <Text
                value={
                  e &&
                  (stopped ? say(v.stopped, { when: stamp(e.disabled_at ?? ""), why: e.disabled_why, key: e.key_name }) : say(v.sending, { key: e.key_name }))
                }
                w="24ch"
              />
            </span>
          </span>
          {e && stopped ? (
            <ActionButton
              size="xs"
              disabled={change.isPending}
              onClick={(ev) => {
                ev.preventDefault();
                change.mutate({ params: { path }, body: { enabled: true } });
              }}
            >
              {say(m.sendAgain)}
            </ActionButton>
          ) : (
            e && <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{say(m.change)}</span>
          )}
        </span>
        <span className="flex flex-wrap items-center gap-1.5 border-t border-dashed border-border px-4 py-2.5">
          <Eyebrow>{say(m.sent)}</Eyebrow>
          {e ? (
            e.events.map((n) => (
              <code key={n} className="rounded bg-secondary px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
                {n}
              </code>
            ))
          ) : (
            <Bar w="18ch" />
          )}
        </span>
      </summary>
      {e && (
        <div className="flex flex-col gap-4 rounded-b-[12px] border-t-2 border-ink bg-muted px-4 py-4">
          <form
            className="flex flex-col gap-2"
            onSubmit={(ev) => {
              ev.preventDefault();
              change.mutate({ params: { path }, body: { events: ticked } });
            }}
          >
            <Eyebrow>{say(m.whatItIsSent)}</Eyebrow>
            <EventBoxes kinds={kinds} ticked={ticked} onChange={setTicked} about />
            <div className="flex flex-wrap items-center gap-3">
              <ActionButton size="sm" type="submit" disabled={change.isPending || ticked.length === 0}>
                {say(m.save)}
              </ActionButton>
              <ActionButton
                quiet
                size="sm"
                type="button"
                disabled={rotate.isPending}
                onClick={() => window.confirm(say(v.newSecretAsk, { url: e.url })) && rotate.mutate({ params: { path }, headers: bodiless })}
              >
                {say(m.newSecret)}
              </ActionButton>
              {!stopped && (
                <ActionButton
                  quiet
                  size="sm"
                  type="button"
                  disabled={change.isPending}
                  onClick={() => change.mutate({ params: { path }, body: { enabled: false } })}
                >
                  {say(m.stop)}
                </ActionButton>
              )}
              <ActionButton
                quiet
                size="sm"
                type="button"
                disabled={drop.isPending}
                onClick={() => window.confirm(say(v.removeAsk, { url: e.url })) && drop.mutate({ params: { path } })}
              >
                {say(m.remove)}
              </ActionButton>
              {change.isSuccess && (
                <p className="text-sm text-[var(--ok)]" role="status">
                  {say(m.saved)}
                </p>
              )}
            </div>
            <Failed errors={[change.error, drop.error, rotate.error]} />
          </form>
          {open && <Deliveries id={e.id} />}
        </div>
      )}
    </details>
  );
}

function Deliveries({ id }: { id: number }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/webhooks/{id}/deliveries", { params: { path: { org, id }, query: { limit: deliveriesShown } } });
  const rows = list.data?.items;
  return (
    <div className="flex flex-col gap-1.5">
      <Eyebrow>{say(m.lastSent)}</Eyebrow>
      {rows?.length === 0 && <p className="text-sm text-muted-foreground">{say(m.nothingSent)}</p>}
      {(!rows || rows.length > 0) && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-[12.5px]">
            <thead>
              <tr className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
                <th className="py-1 pr-2 font-medium">{say(m.when)}</th>
                <th className="py-1 pr-2 font-medium">{say(m.event)}</th>
                <th className="py-1 pr-2 font-medium">{say(m.howItWent)}</th>
                <th className="py-1 pr-2 font-medium">{say(m.tries)}</th>
              </tr>
            </thead>
            <tbody>
              {rows
                ? rows.map((d) => <DeliveryRow key={d.id} d={d} />)
                : !list.error && (
                    <Placeholders>
                      <DeliveryRow />
                    </Placeholders>
                  )}
            </tbody>
          </table>
        </div>
      )}
      <Failed errors={[list.error]} />
    </div>
  );
}

function DeliveryRow({ d }: { d?: Delivery }) {
  const say = useIntl().formatMessage;
  const how =
    d &&
    (d.status === "delivered"
      ? say(v.taken, { code: d.last_code })
      : d.status === "failed"
        ? say(v.failed, { code: d.last_code })
        : d.status === "cancelled"
          ? say(m.cancelled)
          : say(v.retrying, { when: stamp(d.next_at), code: d.last_code }));
  return (
    <tr className="border-t border-dashed border-border align-top">
      <td className="py-1.5 pr-2 font-mono">
        <Text value={d && stamp(d.created_at)} w="8ch" />
      </td>
      <td className="py-1.5 pr-2 font-mono">
        <code>
          <Text value={d?.type} w="12ch" />
        </code>
      </td>
      <td className={cn("py-1.5 pr-2 font-semibold", d?.status === "delivered" ? "text-[var(--ok)]" : d && "text-[var(--stop)]")}>
        <Text value={how} w="10ch" />
      </td>
      <td className="py-1.5 pr-2">
        <Text value={d && String(d.attempts)} w="2ch" />
      </td>
    </tr>
  );
}

function AddEndpoint({ keys, kinds, onSecret }: { keys: Key[]; kinds: EventType[]; onSecret: (s: string) => void }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const reading = keys.filter((k) => k.scopes.includes("read"));
  const [url, setURL] = useState("");
  const [key, setKey] = useState<number>();
  const [ticked, setTicked] = useState<string[]>([]);
  const chosen = key ?? reading[0]?.id;
  const add = $api.useMutation("post", "/o/{org}/api/v1/webhooks", {
    onSuccess: (d) => {
      onSecret(d.secret);
      setURL("");
      setTicked([]);
      refresh();
    },
  });
  return (
    <form
      className="flex flex-col gap-3 rounded-lg border border-dashed border-border p-4"
      onSubmit={(ev) => {
        ev.preventDefault();
        if (chosen) add.mutate({ params: { path: { org } }, body: { url, key: chosen, events: ticked } });
      }}
    >
      <span className="font-heading text-[15px] font-semibold">{say(m.addEndpoint)}</span>
      <label className="flex flex-col gap-1.5">
        <Eyebrow>{say(m.address)}</Eyebrow>
        <Input type="url" required placeholder={exampleURL} value={url} onChange={(ev) => setURL(ev.target.value)} className={cn(fieldClass, "font-mono")} />
        <Meta>{say(m.addressHint)}</Meta>
      </label>
      <label className="flex flex-col gap-1.5">
        <Eyebrow>{say(m.key)}</Eyebrow>
        {reading.length > 0 ? (
          <select className={fieldClass} value={chosen} onChange={(ev) => setKey(Number(ev.target.value))}>
            {reading.map((k) => (
              <option key={k.id} value={k.id}>
                {k.name}
              </option>
            ))}
          </select>
        ) : (
          <p className="text-sm">{say(m.noReadingKey)}</p>
        )}
        <Meta>{say(m.keyHint)}</Meta>
      </label>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1">
          <Eyebrow>{say(m.events)}</Eyebrow>
        </legend>
        <EventBoxes kinds={kinds} ticked={ticked} onChange={setTicked} />
      </fieldset>
      <div>
        <ActionButton size="sm" type="submit" disabled={add.isPending || !chosen || ticked.length === 0}>
          {say(m.add)}
        </ActionButton>
      </div>
      <Failed errors={[add.error]} />
    </form>
  );
}
