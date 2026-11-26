import { Link } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, Pill, Section, useRefresh } from "@/components/app";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, plural } from "@/lib/format";
import { useSession } from "@/session";

type Settings = components["schemas"]["Settings"];
type Me = components["schemas"]["Me"];
type Member = components["schemas"]["Member"];
type MailServer = components["schemas"]["MailServer"];
type SSO = components["schemas"]["SSO"];
type Suppression = components["schemas"]["Suppression"];
type Offering = components["schemas"]["Offering"];

const field = "h-10 rounded-lg border border-input bg-muted px-3 text-sm disabled:opacity-60";
const area = "rounded-lg border border-input bg-muted px-3 py-2 text-sm leading-relaxed disabled:opacity-60";
const select = "h-10 rounded-lg border border-input bg-muted px-3 text-sm";
const saved = "Saved ✓";
// openapi-fetch sends no Content-Type without a body, and the server takes a signed-in
// POST only as JSON, so one with nothing to send says so itself.
const bodiless = { "Content-Type": "application/json" };

const areas = [
  { name: "sales", what: "research, outreach, deals and tasks for agents" },
  { name: "support", what: "enquiries and their replies, subscriptions and onboarding" },
] as const;

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function longDay(at: string) {
  const d = new Date(at);
  return `${d.getDate()} ${months[d.getMonth()]} ${d.getFullYear()}`;
}

function text(form: FormData, name: string) {
  return String(form.get(name) ?? "");
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid grid-cols-1 items-start gap-1.5 sm:grid-cols-[170px_1fr] sm:gap-4">
      <span className="pt-2.5">
        <Eyebrow>{label}</Eyebrow>
      </span>
      <span className="flex flex-col gap-1">{children}</span>
    </label>
  );
}

function Said({ children, className = "text-sm" }: { children: ReactNode; className?: string }) {
  return (
    <p className={`${className} text-[var(--ok)]`} role="status">
      {children}
    </p>
  );
}

function Tick({ name, checked, children }: { name: string; checked: boolean; children: ReactNode }) {
  return (
    <span className="flex items-center gap-2.5 pt-2 text-sm">
      <Checkbox name={name} defaultChecked={checked} />
      {children}
    </span>
  );
}

export function SettingsPage() {
  const { org, owner } = useSession();
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const me = $api.useQuery("get", "/o/{org}/api/v1/me", { params: { path: { org } } });
  const name = settings.data?.name ?? "";
  return (
    <>
      <PageHead title="Settings">
        {settings.data && (
          <span className="text-sm text-muted-foreground">
            For <b className="text-foreground">{name}</b> only
          </span>
        )}
      </PageHead>
      <div className="flex max-w-3xl flex-col gap-5">
        <Failed errors={[settings.error, me.error]} />
        {me.data && <Voice me={me.data} />}
        {me.data && <Password me={me.data} />}
        {settings.data && <Organisation settings={settings.data} />}
        {settings.data && <WhatYouSell orgName={name} />}
        {owner && settings.data && <OutgoingMail orgName={name} />}
        {owner && (
          <Section title="Agents">
            <p className="text-sm">
              Prospector's own agents, and keys for yours, are set up on{" "}
              <Link to="/o/$org/agents" params={{ org }} className="text-mark underline">
                Agents
              </Link>
              .
            </p>
          </Section>
        )}
        {settings.data && <Sequence settings={settings.data} />}
        <NotToBeMailed />
        {owner && <SingleSignOn />}
        {me.data && settings.data && <Team me={me.data} orgName={name} />}
        {owner && <YourData />}
      </div>
    </>
  );
}

function Voice({ me }: { me: Me }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const save = $api.useMutation("patch", "/o/{org}/api/v1/me", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({ params: { path: { org } }, body: { caller_name: text(f, "caller_name"), signature: text(f, "signature") } });
  };
  return (
    <form onSubmit={submit}>
      <Section title="Your voice" hint="Yours in this organisation: used in call scripts and added to copied emails.">
        <Row label="Caller name">
          <Input name="caller_name" defaultValue={me.caller_name} placeholder={me.name} className={field} />
        </Row>
        <Row label="Signature">
          <Textarea name="signature" rows={4} defaultValue={me.signature} className={area} />
        </Row>
        <div className="flex items-center gap-3">
          <ActionButton type="submit" size="sm" disabled={save.isPending}>
            Save
          </ActionButton>
          {save.isSuccess && <Said>{saved}</Said>}
        </div>
        <Failed errors={[save.error]} />
      </Section>
    </form>
  );
}

function Password({ me }: { me: Me }) {
  const { org } = useSession();
  const change = $api.useMutation("post", "/o/{org}/api/v1/me/password");
  if (!me.has_password) {
    return (
      <Section
        title="Your sign-in"
        hint="You sign in through a provider, so there is no password here to change. Whoever runs this server can give you one."
      >
        {null}
      </Section>
    );
  }
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    change.mutate({ params: { path: { org } }, body: { current: text(f, "current"), new: text(f, "new") } }, { onSuccess: () => form.reset() });
  };
  return (
    <form onSubmit={submit}>
      <Section title="Your password" hint="For your sign-in across every organisation. Changing it signs out your other sessions.">
        <Row label="Current">
          <Input name="current" type="password" required autoComplete="current-password" className={field} />
        </Row>
        <Row label="New">
          <Input name="new" type="password" required minLength={10} autoComplete="new-password" className={field} />
        </Row>
        <div className="flex items-center gap-3">
          <ActionButton type="submit" size="sm" disabled={change.isPending}>
            Change
          </ActionButton>
          {change.data && <Said>{change.data.message}</Said>}
        </div>
        <Failed errors={[change.error]} />
      </Section>
    </form>
  );
}

function neverContactHint(covered: string[], total: number) {
  const hint =
    "One per line: a domain, an email address or a LinkedIn profile link. Applied to everything people and agents add, and to who comes up for outreach on Today.";
  if (total === 0) return hint;
  const shown = covered.slice(0, 8);
  let more = ` It currently keeps ${plural(total, "person", "people")} already here out of the queue: ${shown.join(", ")}`;
  if (total > shown.length) more += ` and ${total - shown.length} more`;
  return hint + more + ".";
}

function Organisation({ settings: set }: { settings: Settings }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const covers = $api.useQuery("get", "/o/{org}/api/v1/never-contact/covers", { params: { path: { org }, query: { limit: 8 } } });
  const save = $api.useMutation("patch", "/o/{org}/api/v1/settings", { onSuccess: refresh });
  const intake = `${window.location.origin}/o/${org}/intake`;
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        name: text(f, "name"),
        color: text(f, "color"),
        email_after_days: Number(text(f, "email_after_days")),
        followup_after_days: Number(text(f, "followup_after_days")),
        note_template: text(f, "note_template"),
        pitch: text(f, "pitch"),
        never_contact: text(f, "never_contact"),
        look_for: text(f, "look_for"),
        agent_guidance: text(f, "agent_guidance"),
        enquiry_sites: text(f, "enquiry_sites"),
        onboarding_steps: text(f, "onboarding_steps"),
        currency: text(f, "currency"),
        postal_address: text(f, "postal_address"),
        incorporated_only: f.get("incorporated_only") !== null,
      },
    });
  };
  return (
    <form onSubmit={submit}>
      <Section title="Organisation" hint={owner ? "Shared by everyone in this organisation." : "Only an owner can change these."}>
        <fieldset key={JSON.stringify(set)} disabled={!owner} className="flex flex-col gap-4">
          <Row label="Name">
            <Input name="name" required defaultValue={set.name} className={field} />
          </Row>
          <Row label="Colour">
            <Input name="color" type="color" defaultValue={set.color} className="h-10 w-20 rounded-lg border border-input bg-muted p-1" />
          </Row>
          <Row label="Email after">
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <Input name="email_after_days" type="number" min={0} max={60} defaultValue={set.email_after_days} className={`${field} w-24`} /> days
              without a LinkedIn accept
            </span>
          </Row>
          <Row label="Follow up after">
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <Input name="followup_after_days" type="number" min={0} max={60} defaultValue={set.followup_after_days} className={`${field} w-24`} />{" "}
              days without a reply
            </span>
          </Row>
          <Row label="Connection note">
            <Textarea name="note_template" rows={5} defaultValue={set.note_template} className={area} />
            <Meta>{"{first} {company} {org} are filled in. LinkedIn allows 300 characters."}</Meta>
          </Row>
          <Row label="What you do">
            <Textarea name="pitch" rows={3} defaultValue={set.pitch} className={area} />
            <Meta>{"The call script's pitch line. {company} is filled in."}</Meta>
          </Row>
          <Row label="Never contact">
            <Textarea
              name="never_contact"
              rows={3}
              placeholder="domains, emails or LinkedIn links, one per line"
              defaultValue={set.never_contact}
              className={area}
            />
            <Meta>{neverContactHint(covers.data?.items ?? [], covers.data?.total ?? 0)}</Meta>
          </Row>
          <Row label="What you look for">
            <Textarea
              name="look_for"
              rows={4}
              placeholder={"careers_page: a careers or jobs page on their site\nats: the applicant tracking system they use"}
              defaultValue={set.look_for}
              className={area}
            />
            <Meta>
              One per line, a name and what counts; put (must) after the name of anything a company is no fit without. Agents answer each yes, no or
              unknown with a source, and Research ranks a search's companies by them.
            </Meta>
          </Row>
          <Row label="Agent guidance">
            <Textarea
              name="agent_guidance"
              rows={8}
              placeholder="Who you sell to, who's off-limits, how to judge fit"
              defaultValue={set.agent_guidance}
              className={area}
            />
            <Meta>Sent to every agent when it connects, after the built-in task guidance, and returned by the guidance tool.</Meta>
          </Row>
          <Row label="Enquiry form sites">
            <Textarea name="enquiry_sites" rows={2} placeholder="https://www.example.com" defaultValue={set.enquiry_sites} className={area} />
            <Meta>
              One per line. A form on these sites can POST an enquiry as JSON to {intake} and it shows under Enquiries. Leave empty to accept none.
            </Meta>
          </Row>
          <Row label="Onboarding steps">
            <Textarea
              name="onboarding_steps"
              rows={6}
              placeholder={"them: Name a project lead, 2 days\nthem: Send your product files, 7 days\nus: Kick-off call, 10 days\nus: Go live, 30 days"}
              defaultValue={set.onboarding_steps}
              className={area}
            />
            <Meta>
              What a new customer goes through, one step a line, in order. Start a line with them: or us: for who does it (us when left out), and end
              it with a number of days after the start when it is due. A customer counts as live once every step is done. Changing these leaves
              onboardings already running as they are.
            </Meta>
          </Row>
          <Row label="Currency">
            <Input name="currency" defaultValue={set.currency} maxLength={3} className={`${field} w-24`} />
            <Meta>
              The three-letter code deals are valued in, such as GBP, EUR or USD. Changing it leaves the deals you already valued as they are.
            </Meta>
          </Row>
          <Row label="Postal address">
            <Textarea
              name="postal_address"
              rows={3}
              maxLength={300}
              placeholder={"Alpha Ltd\n1 Example Street\nLeith EH6 1AA"}
              defaultValue={set.postal_address}
              className={area}
            />
            <Meta>
              Where you can be written to. It goes at the foot of every campaign, as the law on marketing mail expects, and a campaign can't be sent
              without it.
            </Meta>
          </Row>
          <Row label="Cold email">
            <Tick name="incorporated_only" checked={set.incorporated_only}>
              Only email incorporated companies (UK PECR)
            </Tick>
          </Row>
          <div className="flex items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={save.isPending}>
              Save
            </ActionButton>
            {save.isSuccess && <Said>{saved}</Said>}
          </div>
        </fieldset>
        <Failed errors={[save.error, covers.error]} />
      </Section>
    </form>
  );
}

function mailHint(m: MailServer) {
  const hint = "This organisation's own mail server. Invitations to new members leave through it";
  if (m.host !== "") return hint + ".";
  if (m.site_mail) return hint + "; without one, they come from this site's address.";
  return hint + "; without one, a new member's first password is shown to you instead.";
}

function OutgoingMail({ orgName }: { orgName: string }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const mail = $api.useQuery("get", "/o/{org}/api/v1/mail-server", { params: { path: { org } } });
  const save = $api.useMutation("put", "/o/{org}/api/v1/mail-server", { onSuccess: refresh });
  const test = $api.useMutation("post", "/o/{org}/api/v1/mail-server/test");
  if (!mail.data) return <Failed errors={[mail.error]} />;
  const m = mail.data;
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        host: text(f, "host"),
        port: Number(text(f, "port")),
        username: text(f, "username"),
        password: text(f, "password"),
        sender: text(f, "sender"),
      },
    });
  };
  return (
    <>
      <form onSubmit={submit}>
        <Section title="Outgoing mail" hint={mailHint(m)}>
          <div key={JSON.stringify(m)} className="flex flex-col gap-4">
            <Row label="Server">
              <span className="flex items-center gap-2">
                <Input name="host" defaultValue={m.host} placeholder="smtp.example.com" className={`${field} min-w-[200px] flex-1`} />
                <Input name="port" type="number" min={1} max={65535} defaultValue={m.port} aria-label="Port" className={`${field} w-24`} />
              </span>
              <Meta>SMTP over TLS, on port 587, 465, 2525 or 25. Clear the server to stop using it.</Meta>
            </Row>
            <Row label="Username">
              <Input name="username" defaultValue={m.username} autoComplete="off" className={field} />
            </Row>
            <Row label="Password">
              <Input
                name="password"
                type="password"
                autoComplete="new-password"
                placeholder={m.has_password ? "saved: leave empty to keep it" : ""}
                className={field}
              />
              <Meta>Never shown again. Changing the server or username needs it entered again.</Meta>
            </Row>
            <Row label="From">
              <Input name="sender" defaultValue={m.sender} placeholder={`${orgName} <hello@example.com>`} className={field} />
            </Row>
          </div>
          <div className="flex items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={save.isPending}>
              Save
            </ActionButton>
            {save.data && <Said>{save.data.host === "" ? "No mail server of this organisation's own." : saved}</Said>}
          </div>
          <Failed errors={[save.error]} />
        </Section>
      </form>
      {(m.host !== "" || m.site_mail) && (
        <form
          className="flex flex-wrap items-center gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            test.mutate({ params: { path: { org } }, headers: bodiless });
          }}
        >
          <ActionButton quiet type="submit" size="sm" disabled={test.isPending}>
            Send me a test email
          </ActionButton>
          {test.data && <Said>{test.data.message}</Said>}
          <Failed errors={[test.error]} />
        </form>
      )}
    </>
  );
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex items-center gap-2.5 rounded-lg border border-border bg-muted px-3 py-2.5">
      <span className="grid size-5 place-items-center rounded-full bg-secondary font-mono text-xs">{n}</span>
      {children}
    </li>
  );
}

function Wait({ children }: { children: ReactNode }) {
  return <li className="pl-8 font-mono text-xs text-mark">{children}</li>;
}

function Sequence({ settings: set }: { settings: Settings }) {
  return (
    <Section title="Sequence" hint="Every contact goes through these steps; the waits come from above.">
      <ol className="flex max-w-lg flex-col gap-1 text-sm">
        <Step n={1}>LinkedIn connection request</Step>
        <Wait>{"↓ on accept, or after " + plural(set.email_after_days, "day")}</Wait>
        <Step n={2}>LinkedIn message if accepted, else email</Step>
        <Wait>{"↓ " + plural(set.followup_after_days, "day") + " without a reply"}</Wait>
        <Step n={3}>Follow-up email</Step>
        <Wait>↓ whenever a number is known, in office hours</Wait>
        <Step n={4}>Call</Step>
      </ol>
    </Section>
  );
}

function suppressionMeta(r: Suppression) {
  if (r.reason === "unsubscribed") return "unsubscribed " + longDay(r.created_at);
  if (r.reason === "bounced") return "bounced " + longDay(r.created_at);
  if (r.reason === "complained") return "marked a mail as spam " + longDay(r.created_at);
  return "added by " + actor(r.by) + " " + longDay(r.created_at);
}

function OfferingFields({ o }: { o?: Offering }) {
  return (
    <>
      <Row label="Name">
        <Input name="name" required maxLength={60} defaultValue={o?.name} placeholder="Contract roles" className={field} />
      </Row>
      <Row label="What you look for">
        <Textarea name="look_for" rows={4} defaultValue={o?.look_for} placeholder={"ir35: outside IR35, or not stated\nopen_roles (must): a contract role open now"} className={area} />
        <Meta>As above, one per line. Agents on a task for this offering check these in place of the organisation's own.</Meta>
      </Row>
      <Row label="What you do">
        <Textarea name="pitch" rows={2} defaultValue={o?.pitch} className={area} />
        <Meta>{"The call script's pitch line for people reached for it. Empty uses the organisation's."}</Meta>
      </Row>
      <Row label="Connection note">
        <Textarea name="note_template" rows={3} defaultValue={o?.note_template} className={area} />
        <Meta>{"For people picked for it. Empty uses the organisation's. {first} {company} {org} are filled in."}</Meta>
      </Row>
    </>
  );
}

function offeringBody(form: HTMLFormElement) {
  const f = new FormData(form);
  return { name: text(f, "name"), look_for: text(f, "look_for"), pitch: text(f, "pitch"), note_template: text(f, "note_template") };
}

function WhatYouSell({ orgName }: { orgName: string }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/offerings", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/offerings", { onSuccess: refresh });
  const [adding, setAdding] = useState(false);
  const rows = list.data?.items ?? [];
  return (
    <Section
      title="What you sell"
      hint={`Everything above is ${orgName}'s own offering. Add another when you sell something else to companies you already know: each one is reviewed for on its own, with its own look-for fields, pitch and note, while facts and people are shared.`}
    >
      {list.data && rows.length === 0 && <p className="text-sm text-muted-foreground">Only {orgName}'s own.</p>}
      {rows.map((o) => (
        <OfferingRow key={o.id} o={o} />
      ))}
      {owner &&
        (adding ? (
          <form
            className="flex flex-col gap-4 rounded-lg border border-border p-4"
            onSubmit={(e) => {
              e.preventDefault();
              add.mutate({ params: { path: { org } }, body: offeringBody(e.currentTarget) }, { onSuccess: () => setAdding(false) });
            }}
          >
            <OfferingFields />
            <div className="flex items-center gap-2">
              <ActionButton type="submit" size="sm" disabled={add.isPending}>
                Add
              </ActionButton>
              <ActionButton quiet type="button" size="sm" onClick={() => setAdding(false)}>
                Cancel
              </ActionButton>
            </div>
          </form>
        ) : (
          <div>
            <ActionButton quiet size="sm" onClick={() => setAdding(true)}>
              + Add an offering
            </ActionButton>
          </div>
        ))}
      <Failed errors={[list.error, add.error]} />
    </Section>
  );
}

function OfferingRow({ o }: { o: Offering }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const change = $api.useMutation("patch", "/o/{org}/api/v1/offerings/{id}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/offerings/{id}", { onSuccess: refresh });
  const fields = o.looking_for.map((a) => a.field);
  return (
    <details className="group rounded-lg border border-border">
      <summary className="flex cursor-pointer list-none flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
        <span className="font-heading font-semibold">{o.name}</span>
        <span className="min-w-0 font-mono text-xs text-muted-foreground">
          {fields.length === 0 ? "nothing to look for yet" : fields.slice(0, 4).join(" · ") + (fields.length > 4 ? ` · +${fields.length - 4} more` : "")}
        </span>
        <span className="ml-auto font-mono text-xs text-muted-foreground">{owner ? "Edit ›" : "›"}</span>
      </summary>
      <form
        className="flex flex-col gap-4 border-t border-border px-4 py-4"
        onSubmit={(e) => {
          e.preventDefault();
          change.mutate({ params: { path: { org, id: o.id } }, body: offeringBody(e.currentTarget) });
        }}
      >
        <fieldset key={o.updated_at} disabled={!owner} className="flex flex-col gap-4">
          <OfferingFields o={o} />
          {owner && (
            <div className="flex flex-wrap items-center gap-2">
              <ActionButton type="submit" size="sm" disabled={change.isPending}>
                Save
              </ActionButton>
              {change.isSuccess && <Said>{saved}</Said>}
              <ActionButton
                quiet
                type="button"
                size="sm"
                className="ml-auto"
                onClick={() =>
                  window.confirm(`Remove ${o.name}? Where companies stand for it is forgotten, though their history keeps it, and people reached for it are then reached for the organisation's own.`) &&
                  remove.mutate({ params: { path: { org, id: o.id } } })
                }
              >
                Remove
              </ActionButton>
            </div>
          )}
          <Meta>Changed by {actor(o.updated_by)}</Meta>
        </fieldset>
        <Failed errors={[change.error, remove.error]} />
      </form>
    </details>
  );
}

function NotToBeMailed() {
  const { org, sales, support } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/suppressions", { params: { path: { org } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/suppressions", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/suppressions/{id}", { onSuccess: refresh });
  const rows = list.data?.items ?? [];
  return (
    <Section
      title="Not to be mailed"
      hint="Marketing mail never goes to these addresses. Someone who unsubscribes by the link in a mail lands here and stays: only they can undo it. Your never-contact list and anyone erased are kept out as well."
    >
      {list.data && rows.length === 0 && <p className="text-sm text-muted-foreground">Nobody yet.</p>}
      {rows.length > 0 && (
        <div>
          {rows.map((r) => (
            <div key={r.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-1.5 text-sm">
              <span>
                <b>{r.address}</b> <span className="font-mono text-xs text-muted-foreground">{suppressionMeta(r)}</span>
              </span>
              {r.reason === "added" && sales && (
                <ActionButton
                  quiet
                  size="xs"
                  onClick={() => window.confirm(`Mail ${r.address} again?`) && remove.mutate({ params: { path: { org, id: r.id } } })}
                >
                  Take off
                </ActionButton>
              )}
            </div>
          ))}
        </div>
      )}
      {list.data?.next_cursor && <Meta>The newest are shown. The API lists them all.</Meta>}
      {(sales || support) && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const form = e.currentTarget;
            add.mutate({ params: { path: { org } }, body: { address: text(new FormData(form), "address") } }, { onSuccess: () => form.reset() });
          }}
        >
          <Input name="address" type="email" required placeholder="someone@example.com" aria-label="Address not to mail" className={`${field} w-auto`} />
          <ActionButton quiet type="submit" size="sm" disabled={add.isPending}>
            Add
          </ActionButton>
        </form>
      )}
      <Failed errors={[list.error, add.error, remove.error]} />
    </Section>
  );
}

function SingleSignOn() {
  const { org } = useSession();
  const sso = $api.useQuery("get", "/o/{org}/api/v1/sso", { params: { path: { org } } });
  return (
    <Section
      title="Single sign-on"
      hint="Let people at your company's email domains sign in through your own provider: Okta, Entra ID, Google Workspace or anything else that speaks OpenID Connect."
    >
      {sso.data && sso.data.redirect_uri === "" && (
        <Meta>Set PROSPECTOR_URL to this app's address first: it is where the provider sends people back.</Meta>
      )}
      {sso.data && sso.data.redirect_uri !== "" && (
        <>
          <Domains />
          <Provider sso={sso.data} />
        </>
      )}
      <Failed errors={[sso.error]} />
    </Section>
  );
}

function Domains() {
  const { org } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/sso/domains", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/sso/domains", { onSuccess: refresh });
  const verify = $api.useMutation("post", "/o/{org}/api/v1/sso/domains/{domain}/verify", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/sso/domains/{domain}", { onSuccess: refresh });
  return (
    <div className="flex flex-col gap-2">
      <Eyebrow>Your domains</Eyebrow>
      {list.data?.items.map((d) => (
        <div key={d.domain} className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-1.5 text-sm">
          <span className="flex items-center gap-2">
            <b>{d.domain}</b>
            {d.verified_at ? <Pill tone="ok">verified</Pill> : <Pill tone="warn">not verified</Pill>}
          </span>
          <span className="flex items-center gap-3">
            {!d.verified_at && (
              <ActionButton size="xs" onClick={() => verify.mutate({ params: { path: { org, domain: d.domain } }, headers: bodiless })}>
                Verify
              </ActionButton>
            )}
            <ActionButton
              quiet
              size="xs"
              onClick={() =>
                window.confirm(`Remove ${d.domain}? People at it can no longer use single sign-on here.`) &&
                remove.mutate({ params: { path: { org, domain: d.domain } } })
              }
            >
              Remove
            </ActionButton>
          </span>
          {!d.verified_at && (
            <p className="w-full text-muted-foreground">
              Add a TXT record named <code className="select-all break-all font-mono text-xs text-foreground">{d.record}</code> with the value{" "}
              <code className="select-all break-all font-mono text-xs text-foreground">{d.token}</code>, then verify.
            </p>
          )}
        </div>
      ))}
      <Failed errors={[list.error, verify.error, remove.error]} />
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const form = e.currentTarget;
          add.mutate({ params: { path: { org } }, body: { domain: text(new FormData(form), "domain") } }, { onSuccess: () => form.reset() });
        }}
      >
        <label className="sr-only" htmlFor="sso-domain">
          Email domain
        </label>
        <Input id="sso-domain" name="domain" required placeholder="example.com" className={`${field} min-w-[200px] flex-1`} />
        <ActionButton quiet type="submit" size="sm" className="h-10" disabled={add.isPending}>
          Add domain
        </ActionButton>
        <Failed errors={[add.error]} />
      </form>
      <Meta>Single sign-on only ever speaks for addresses at a verified domain, matched whole: example.com doesn't cover mail.example.com.</Meta>
    </div>
  );
}

function Provider({ sso }: { sso: SSO }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const save = $api.useMutation("put", "/o/{org}/api/v1/sso", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        issuer: text(f, "issuer"),
        client_id: text(f, "client_id"),
        client_secret: text(f, "client_secret"),
        required: f.get("required") !== null,
        auto_join: f.get("auto_join") !== null,
      },
    });
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div key={JSON.stringify(sso)} className="flex flex-col gap-4">
        <Row label="Issuer">
          <Input name="issuer" type="url" defaultValue={sso.issuer} placeholder="https://login.example.com" className={field} />
          <Meta>Your provider's OpenID Connect issuer. Empty turns single sign-on off.</Meta>
        </Row>
        <Row label="Client ID">
          <Input name="client_id" defaultValue={sso.client_id} autoComplete="off" className={field} />
        </Row>
        <Row label="Client secret">
          <Input
            name="client_secret"
            type="password"
            autoComplete="new-password"
            placeholder={sso.has_client_secret ? "Kept. Type a new one to replace it" : ""}
            className={field}
          />
          {!sso.sealing && (
            <Meta>
              A secret is kept sealed, so none can be saved until whoever runs this server sets PROSPECTOR_SECRET_KEYS (`prospector secret-key` makes
              one). Everything else here can still be changed.
            </Meta>
          )}
        </Row>
        <Row label="Redirect URI">
          <code className="select-all break-all pt-2 font-mono text-xs">{sso.redirect_uri}</code>
          <Meta>Register this with your provider as the place to send people back.</Meta>
        </Row>
        <Row label="Require it">
          <Tick name="required" checked={sso.required}>
            Members at your domains must sign in this way
          </Tick>
          <Meta>Owners can still use a password, so a broken provider can be put right. API keys are not affected.</Meta>
        </Row>
        <Row label="New people">
          <Tick name="auto_join" checked={sso.auto_join}>
            Anyone at your domains becomes a member when they first sign in
          </Tick>
          <Meta>Never an owner. Someone you remove here joins again while your provider still lets them in: remove them there too.</Meta>
        </Row>
      </div>
      <div className="flex items-center gap-3">
        <ActionButton type="submit" size="sm" disabled={save.isPending}>
          Save
        </ActionButton>
        {save.isSuccess && <Said>{saved}</Said>}
      </div>
      <Failed errors={[save.error]} />
    </form>
  );
}

function memberWork(m: Member) {
  if (m.role === "owner") return "owner";
  if (m.areas.length === 0) return "member · reads only";
  return "member · " + m.areas.join(", ");
}

function otherRole(role: string) {
  return role === "owner" ? "member" : "owner";
}

function roleConfirm(orgName: string, me: Me, m: Member) {
  if (m.role !== "owner") return `Make ${m.email} an owner of ${orgName}? They can change settings, keys and the team, export and erase.`;
  if (m.user_id === me.user_id) return `Stop being an owner of ${orgName}? Only another owner can make you one again.`;
  return `Make ${m.email} a member? They stop being able to change settings, keys and the team.`;
}

function removeConfirm(orgName: string, me: Me, m: Member) {
  if (m.user_id === me.user_id) return `Leave ${orgName}? You lose access to it.`;
  return `Remove ${m.email} from ${orgName}? They lose access to it; their account and what they added stay, and so do any API keys they made.`;
}

function Team({ me, orgName }: { me: Me; orgName: string }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const members = $api.useQuery("get", "/o/{org}/api/v1/members", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/members", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    const role = text(f, "role") === "owner" ? "owner" : "member";
    add.mutate({ params: { path: { org } }, body: { email: text(f, "email"), name: text(f, "name"), role } }, { onSuccess: () => form.reset() });
  };
  return (
    <Section title="Team">
      <div>
        {members.data?.items.map((m) => (
          <TeamMember key={m.user_id} m={m} me={me} orgName={orgName} />
        ))}
      </div>
      <Failed errors={[members.error]} />
      {owner && (
        <form onSubmit={submit} className="flex flex-wrap gap-2">
          <label className="sr-only" htmlFor="m-email">
            Email
          </label>
          <Input id="m-email" name="email" type="email" required placeholder="email" className={`${field} min-w-[200px] flex-1`} />
          <label className="sr-only" htmlFor="m-name">
            Name
          </label>
          <Input id="m-name" name="name" placeholder="name" className={`${field} w-40`} />
          <label className="sr-only" htmlFor="m-role">
            Role
          </label>
          <select id="m-role" name="role" className={select}>
            <option value="member">member</option>
            <option value="owner">owner</option>
          </select>
          <ActionButton quiet type="submit" size="sm" className="h-10" disabled={add.isPending}>
            Add
          </ActionButton>
          {add.data && <Said className="w-full select-all font-mono text-sm">{add.data.message}</Said>}
          <Failed errors={[add.error]} />
        </form>
      )}
    </Section>
  );
}

function TeamMember({ m, me, orgName }: { m: Member; me: Me; orgName: string }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const self = m.user_id === me.user_id;
  const path = { params: { path: { org, user: m.user_id } } };
  // Your own role is in the page's session, so a change to it is seen by loading the page again.
  const role = $api.useMutation("patch", "/o/{org}/api/v1/members/{user}", { onSuccess: () => (self ? window.location.reload() : refresh()) });
  const work = $api.useMutation("patch", "/o/{org}/api/v1/members/{user}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/members/{user}", {
    onSuccess: () => (self ? window.location.assign("/") : refresh()),
  });
  const [picked, setPicked] = useState<string[]>(m.areas);
  const toggle = (name: string, on: boolean) => setPicked(on ? [...picked, name] : picked.filter((a) => a !== name));
  return (
    <div className="flex flex-wrap justify-between border-b border-dashed border-border py-1.5 text-sm">
      <span>
        <b>{m.name}</b> <span className="text-muted-foreground">{m.email}</span>
      </span>
      <span className="flex items-center gap-3">
        <Meta>{memberWork(m)}</Meta>
        {owner && (
          <>
            <ActionButton
              quiet
              size="xs"
              onClick={() => window.confirm(roleConfirm(orgName, me, m)) && role.mutate({ ...path, body: { role: otherRole(m.role) } })}
            >
              Make {otherRole(m.role)}
            </ActionButton>
            <ActionButton quiet size="xs" onClick={() => window.confirm(removeConfirm(orgName, me, m)) && remove.mutate(path)}>
              Remove
            </ActionButton>
          </>
        )}
      </span>
      {owner && m.role !== "owner" && (
        <form
          className="flex w-full flex-wrap items-center gap-3 pt-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            work.mutate({ ...path, body: { areas: areas.map((a) => a.name).filter((a) => picked.includes(a)) } });
          }}
        >
          {areas.map((a) => (
            <label key={a.name} className="flex items-center gap-1.5 text-xs text-muted-foreground" title={a.what}>
              <Checkbox checked={picked.includes(a.name)} onCheckedChange={(on) => toggle(a.name, on === true)} />
              {a.name}
            </label>
          ))}
          <ActionButton quiet type="submit" size="xs" disabled={work.isPending}>
            Save
          </ActionButton>
          {work.isSuccess && <Said className="text-xs">{saved}</Said>}
        </form>
      )}
      <Failed errors={[role.error, work.error, remove.error]} />
    </div>
  );
}

function YourData() {
  const { org } = useSession();
  const exported = $api.useMutation("post", "/o/{org}/api/v1/export", {
    onSuccess: (data) => {
      const a = document.createElement("a");
      a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
      a.download = `${org}.json`;
      a.click();
      URL.revokeObjectURL(a.href);
    },
  });
  return (
    <Section title="Your data">
      <p className="text-sm text-muted-foreground">
        Everything this organisation holds here, in one file: companies, people, facts, enquiries, history, settings and who has access. It holds no
        passwords, keys or sign-on secrets.
      </p>
      <div>
        <ActionButton quiet size="sm" disabled={exported.isPending} onClick={() => exported.mutate({ params: { path: { org } }, headers: bodiless })}>
          Download everything
        </ActionButton>
      </div>
      <Failed errors={[exported.error]} />
    </Section>
  );
}
