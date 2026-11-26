import { type ReactNode, useEffect, useRef, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, PageHead } from "@/components/app";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { problemText } from "@/problem";
import { useSession } from "@/session";

type Draft = components["schemas"]["PersonDraft"];
type Settings = components["schemas"]["Settings"];

const field = "h-10 rounded-lg border-input bg-muted px-3 text-sm";

// AddPage is the form for someone found by hand. With ?popup=1 it is the small window the
// bookmarklet opens from a LinkedIn profile, which closes itself once they are added.
export function AddPage() {
  const { org, sales } = useSession();
  const { popup = false, url = "", name = "", headline = "", company = "" } = useSearch({ from: "/o/$org/add" });
  const query = { url, name, headline, company };
  const draft = $api.useQuery("get", "/o/{org}/api/v1/people/draft", { params: { path: { org }, query } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  if (!draft.data || !settings.data) return null;

  const form = sales ? (
    <AddForm draft={draft.data} settings={settings.data} popup={popup} />
  ) : (
    <p className="font-mono text-xs text-mark">Adding people is sales work. An owner can add sales to you on the Team list in Settings.</p>
  );
  if (popup) {
    return (
      <>
        <div className="mb-4 flex items-center gap-2.5">
          <OrgMark name={settings.data.name} color={settings.data.color} />
          <h1 className="font-heading text-xl font-bold">Add to {settings.data.name}</h1>
        </div>
        {form}
      </>
    );
  }
  return (
    <>
      <PageHead title="Add person" />
      <div className="flex flex-wrap items-start gap-8">
        <section className="min-w-0 flex-[999_1_480px] panel p-6">{form}</section>
        {sales && <Faster name={settings.data.name} bookmarklet={draft.data.bookmarklet} />}
      </div>
    </>
  );
}

function OrgMark({ name, color }: { name: string; color: string }) {
  return (
    <span className="grid size-7 shrink-0 place-items-center rounded-md font-heading text-sm font-bold text-[#111]" style={{ background: color }}>
      {[...name][0]?.toUpperCase() ?? "?"}
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

function AddForm({ draft, settings, popup }: { draft: Draft; settings: Settings; popup: boolean }) {
  const { org } = useSession();
  const [values, setValues] = useState({ ...blank, linkedin: draft.linkedin, name: draft.name, title: draft.title, company: draft.company });
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
    onError: (error) => setResult({ text: problemText(error, "", 0) || "That didn't work. Reload and try again.", failed: true }),
  });
  const set = (key: keyof typeof blank) => (e: { target: { value: string } }) => setValues({ ...values, [key]: e.target.value });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        add.mutate({ params: { path: { org } }, body: { ...values, added } });
      }}
    >
      <Field label="LinkedIn profile">
        <Input
          ref={first}
          type="url"
          value={values.linkedin}
          onChange={set("linkedin")}
          autoFocus={draft.linkedin === ""}
          placeholder="https://www.linkedin.com/in/…"
          className={field}
        />
      </Field>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label="Name">
          <Input required value={values.name} onChange={set("name")} autoFocus={draft.linkedin !== "" && draft.name === ""} className={field} />
        </Field>
        <Field label="Role">
          <Input value={values.title} onChange={set("title")} placeholder="Head of Marketing" className={field} />
        </Field>
        <Field label="Company">
          <Input value={values.company} onChange={set("company")} className={field} />
        </Field>
        <Field label="Email (optional)">
          <Input type="email" value={values.email} onChange={set("email")} className={field} />
        </Field>
      </div>
      <Field label="Connection note">
        <Textarea
          rows={5}
          maxLength={300}
          value={values.note}
          onChange={set("note")}
          placeholder={"Leave empty to use your template:\n\n" + settings.note_template}
          className="rounded-lg border-input bg-muted px-3 py-2 text-sm leading-relaxed"
        />
      </Field>
      <label className="flex items-center gap-2.5 text-sm">
        <Checkbox checked={added} onCheckedChange={(on) => setAdded(on === true)} />
        I've already sent the connection request
      </label>
      <div className="flex flex-wrap items-center gap-3">
        <ActionButton type="submit" disabled={add.isPending}>
          Add to {settings.name}
        </ActionButton>
        <p className={result?.failed ? "text-sm text-[var(--stop)]" : "text-sm text-[var(--ok)]"} role="status">
          {result?.text}
        </p>
      </div>
    </form>
  );
}

function Faster({ name, bookmarklet }: { name: string; bookmarklet: string }) {
  const link = useRef<HTMLAnchorElement>(null);
  // React refuses a javascript: href, which is all a bookmarklet is, so it is set here.
  useEffect(() => link.current?.setAttribute("href", bookmarklet), [bookmarklet]);
  return (
    <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-3 panel p-5 text-sm">
      <Eyebrow>Faster: from LinkedIn itself</Eyebrow>
      <p className="text-muted-foreground">
        Drag this to your bookmarks bar. On someone's LinkedIn profile, click it: this form opens filled in with their name, role and company.
      </p>
      <a ref={link} className="self-start rounded-lg border border-dashed border-primary px-3 py-2 font-medium text-mark" data-bookmarklet draggable="true">
        + {name}
      </a>
      <p className="text-muted-foreground">It reads only the profile's title and headline, when you click. Nothing runs on LinkedIn by itself.</p>
      <Eyebrow>Then</Eyebrow>
      <p className="text-muted-foreground">
        They land in Today under LinkedIn adds, with your note ready: <b className="text-foreground">o</b> opens the profile and copies the note,{" "}
        <b className="text-foreground">a</b> marks them added.
      </p>
    </aside>
  );
}
