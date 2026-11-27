import { type ReactNode, useEffect, useRef, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, PageHead, usePageTitle } from "@/components/app";
import { Loading, Text } from "@/components/app/skeleton";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { problemText } from "@/problem";
import { useSession } from "@/session";

type Draft = components["schemas"]["PersonDraft"];
type Settings = components["schemas"]["Settings"];

const field = "h-10 rounded-lg border-input bg-muted px-3 text-sm";

// What a LinkedIn profile's address looks like: an address, not words.
const profileExample = "https://www.linkedin.com/in/…";

const m = defineMessages({
  title: { defaultMessage: "Add person", id: "rKDuXV", description: "Title of the page to add a person by hand" },
  notSales: {
    defaultMessage: "Adding people is sales work. An owner can add sales to you on the Team list in Settings.",
    id: "gA7E5s",
    description: "On the add person page, for a member who doesn't do sales",
  },
  linkedin: { defaultMessage: "LinkedIn profile", id: "S3a3KE", description: "Label of the field for a person's LinkedIn profile address" },
  name: { defaultMessage: "Name", id: "SaS5to", description: "Label of the field for a person's name" },
  role: { defaultMessage: "Role", id: "OVGwgM", description: "Label of the field for a person's job title" },
  roleExample: { defaultMessage: "Head of Marketing", id: "qCBlOC", description: "Example job title shown in the empty role field" },
  company: { defaultMessage: "Company", id: "JJI9qp", description: "Label of the field for a person's company" },
  email: { defaultMessage: "Email (optional)", id: "WDiXzK", description: "Label of the field for a person's email address" },
  note: { defaultMessage: "Connection note", id: "JTms4T", description: "Label of the field for the note sent with a LinkedIn connection request" },
  sent: { defaultMessage: "I've already sent the connection request", id: "rK4VHZ", description: "Checkbox: the LinkedIn request has been sent already" },
  failed: {
    defaultMessage: "That didn't work. Reload and try again.",
    id: "4MZWhE",
    description: "When adding a person failed and the server said nothing about why",
  },
  faster: { defaultMessage: "Faster: from LinkedIn itself", id: "O3c/qR", description: "Heading of the panel about the bookmarklet" },
  drag: {
    defaultMessage: "Drag this to your bookmarks bar. On someone's LinkedIn profile, click it: this form opens filled in with their name, role and company.",
    id: "GAdsC7",
    description: "How to use the bookmarklet",
  },
  reads: {
    defaultMessage: "It reads only the profile's title and headline, when you click. Nothing runs on LinkedIn by itself.",
    id: "HPuNNE",
    description: "What the bookmarklet reads",
  },
  next: { defaultMessage: "Then", id: "rJg0SS", description: "Small heading: what happens after adding someone with the bookmarklet" },
});

const v = {
  template: defineMessage<{ template: string }>({
    defaultMessage: "Leave empty to use your template:\n\n{template}",
    id: "0QRSAG",
    description: "Placeholder of the empty connection note: the organisation's template follows",
  }),
};

const kbd = (c: ReactNode[]) => <b className="text-foreground">{c}</b>;
const plus = "+";
const unnamed = "?";

// AddPage is the form for someone found by hand. With ?popup=1 it is the small window the
// bookmarklet opens from a LinkedIn profile, which closes itself once they are added.
export function AddPage() {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { popup = false, url = "", name = "", headline = "", company = "" } = useSearch({ from: "/o/$org/add" });
  const query = { url, name, headline, company };
  const draft = $api.useQuery("get", "/o/{org}/api/v1/people/draft", { params: { path: { org }, query } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const ready = draft.data && settings.data ? { draft: draft.data, settings: settings.data } : undefined;
  // A failed load shows its error in place of a form that could never fill.
  const failed = !ready && (draft.error || settings.error);

  // Keyed on the data arriving, so the fields start from the draft once there is one.
  const form = failed ? null : sales ? (
    <AddForm key={ready ? "ready" : "loading"} draft={ready?.draft} settings={ready?.settings} popup={popup} />
  ) : (
    <p className="font-mono text-xs text-mark">{say(m.notSales)}</p>
  );
  usePageTitle(say(m.title));
  if (popup) {
    return (
      <Loading busy={!ready && !failed}>
        <div className="mb-4 flex items-center gap-2.5">
          <OrgMark name={settings.data?.name} color={settings.data?.color} />
          <h1 className="font-heading text-xl font-bold">
            <FormattedMessage
              defaultMessage="Add to {org}"
              id="0LnApv"
              description="Heading of the bookmarklet's window, and the button that adds the person. {org} is the organisation's name"
              values={{ org: <Text value={settings.data?.name ?? (failed ? "" : undefined)} w="8ch" /> }}
            />
          </h1>
        </div>
        <Failed errors={[draft.error, settings.error]} />
        {form}
      </Loading>
    );
  }
  return (
    <>
      <PageHead title={say(m.title)} />
      <Failed errors={[draft.error, settings.error]} />
      <Loading busy={!ready && !failed} className="flex flex-wrap items-start gap-8">
        <section className="min-w-0 flex-[999_1_480px] panel p-6">{form}</section>
        {sales && !failed && <Faster name={settings.data?.name} bookmarklet={draft.data?.bookmarklet} />}
      </Loading>
    </>
  );
}

function OrgMark({ name, color }: { name?: string; color?: string }) {
  return (
    <span
      className={cn(
        "grid size-7 shrink-0 place-items-center rounded-md font-heading text-sm font-bold text-[#111]",
        name === undefined && "motion-safe:animate-pulse bg-accent",
      )}
      style={color === undefined ? undefined : { background: color }}
    >
      {name !== undefined && ([...name][0]?.toUpperCase() ?? unnamed)}
    </span>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <Eyebrow>{label}</Eyebrow>
      {children}
    </label>
  );
}

const blank = { linkedin: "", name: "", title: "", company: "", email: "", note: "" };

// AddForm is drawn before the draft and settings come, its fields empty, pulsing and disabled.
function AddForm({ draft, settings, popup }: { draft?: Draft; settings?: Settings; popup: boolean }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const [values, setValues] = useState({
    ...blank,
    linkedin: draft?.linkedin ?? "",
    name: draft?.name ?? "",
    title: draft?.title ?? "",
    company: draft?.company ?? "",
  });
  const [added, setAdded] = useState(false);
  const [result, setResult] = useState<{ text: string; failed: boolean } | null>(null);
  const first = useRef<HTMLInputElement>(null);
  const add = $api.useMutation("post", "/o/{org}/api/v1/people", {
    onSuccess: (person) => {
      setResult({ text: person.message, failed: false });
      setValues(blank);
      first.current?.focus();
      if (popup) setTimeout(() => window.close(), 1200);
    },
    onError: (error) => setResult({ text: problemText(error, "") || say(m.failed), failed: true }),
  });
  const set = (key: keyof typeof blank) => (e: { target: { value: string } }) => setValues({ ...values, [key]: e.target.value });
  const loading = !draft || !settings;
  const box = cn(field, loading && "motion-safe:animate-pulse");

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        add.mutate({ params: { path: { org } }, body: { ...values, added } });
      }}
    >
      <Field label={say(m.linkedin)}>
        <Input
          ref={first}
          type="url"
          value={values.linkedin}
          onChange={set("linkedin")}
          // oxlint-disable-next-line jsx-a11y/no-autofocus -- opened to add someone, the first empty field is where typing starts
          autoFocus={draft?.linkedin === ""}
          disabled={loading}
          placeholder={profileExample}
          className={box}
        />
      </Field>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={say(m.name)}>
          <Input
            required
            value={values.name}
            onChange={set("name")}
            // oxlint-disable-next-line jsx-a11y/no-autofocus -- as above
            autoFocus={draft !== undefined && draft.linkedin !== "" && draft.name === ""}
            disabled={loading}
            className={box}
          />
        </Field>
        <Field label={say(m.role)}>
          <Input value={values.title} onChange={set("title")} placeholder={say(m.roleExample)} disabled={loading} className={box} />
        </Field>
        <Field label={say(m.company)}>
          <Input value={values.company} onChange={set("company")} disabled={loading} className={box} />
        </Field>
        <Field label={say(m.email)}>
          <Input type="email" value={values.email} onChange={set("email")} disabled={loading} className={box} />
        </Field>
      </div>
      <Field label={say(m.note)}>
        <Textarea
          rows={5}
          maxLength={300}
          value={values.note}
          onChange={set("note")}
          placeholder={settings && say(v.template, { template: settings.note_template })}
          disabled={loading}
          className={cn("rounded-lg border-input bg-muted px-3 py-2 text-sm leading-relaxed", loading && "motion-safe:animate-pulse")}
        />
      </Field>
      <label className="flex items-center gap-2.5 text-sm">
        <Checkbox checked={added} disabled={loading} onCheckedChange={(on) => setAdded(on === true)} />
        {say(m.sent)}
      </label>
      <div className="flex flex-wrap items-center gap-3">
        <ActionButton type="submit" disabled={loading || add.isPending}>
          <FormattedMessage
            defaultMessage="Add to {org}"
            id="0LnApv"
            description="Heading of the bookmarklet's window, and the button that adds the person. {org} is the organisation's name"
            values={{ org: <Text value={settings?.name} w="8ch" /> }}
          />
        </ActionButton>
        <p className={result?.failed ? "text-sm text-[var(--stop)]" : "text-sm text-[var(--ok)]"} role="status">
          {result?.text}
        </p>
      </div>
    </form>
  );
}

function Faster({ name, bookmarklet }: { name?: string; bookmarklet?: string }) {
  const say = useIntl().formatMessage;
  const link = useRef<HTMLAnchorElement>(null);
  // React refuses a javascript: href, which is all a bookmarklet is, so it is set here.
  useEffect(() => {
    if (bookmarklet !== undefined) link.current?.setAttribute("href", bookmarklet);
  }, [bookmarklet]);
  return (
    <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-3 panel p-5 text-sm">
      <Eyebrow>{say(m.faster)}</Eyebrow>
      <p className="text-muted-foreground">{say(m.drag)}</p>
      {/* oxlint-disable-next-line jsx-a11y/anchor-is-valid -- its javascript: address is set from an effect, as React won't render one */}
      <a ref={link} className="self-start rounded-lg border border-dashed border-primary px-3 py-2 font-medium text-mark" data-bookmarklet draggable="true">
        {plus} <Text value={name} w="8ch" />
      </a>
      <p className="text-muted-foreground">{say(m.reads)}</p>
      <Eyebrow>{say(m.next)}</Eyebrow>
      <p className="text-muted-foreground">
        <FormattedMessage
          defaultMessage="They land in Today under LinkedIn adds, with your note ready: <o>o</o> opens the profile and copies the note, <a>a</a> marks them added."
          id="+g0jsI"
          description="What happens after adding someone. <o>o</o> and <a>a</a> are the keyboard keys that do it, shown in bold"
          values={{ o: kbd, a: kbd }}
        />
      </p>
    </aside>
  );
}
