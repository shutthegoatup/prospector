import { Link, Outlet, useLocation, useMatch, useNavigate, useParams } from "@tanstack/react-router";
import { type CSSProperties, useEffect, useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { cn } from "@/lib/utils";
import { mustGoFirst } from "@/problem";
import { SessionContext } from "@/session";
import { Bar, Text } from "./skeleton";

type Nav = components["schemas"]["Nav"];
type Area = "sales" | "support";

// The templ app's own button, as the server-rendered pages draw it, so the sidebar looks
// as it did before it moved here.
const button =
  "focus-visible:border-ring focus-visible:ring-ring/50 rounded-lg border-2 border-transparent bg-clip-padding text-sm font-semibold focus-visible:ring-3 active:not-aria-[haspopup]:translate-y-px group/button inline-flex shrink-0 items-center justify-center whitespace-nowrap transition-all outline-none select-none disabled:pointer-events-none disabled:opacity-50";
const primary =
  "border-foreground bg-primary text-primary-foreground shadow-[2px_2px_0_var(--foreground)] hover:-translate-x-px hover:-translate-y-px hover:shadow-[3px_3px_0_var(--foreground)] h-8 gap-1.5 px-2.5";
const outlineSm =
  "border-foreground bg-card hover:bg-secondary hover:text-foreground h-7 gap-1 rounded-[min(var(--radius-md),12px)] px-2.5 text-[0.8rem]";

// pageArea is the area whose work each sidebar page is; someone without it reads only.
const pageArea: Partial<Record<string, Area>> = {
  today: "sales",
  research: "sales",
  conversations: "sales",
  campaigns: "sales",
  tasks: "sales",
  enquiries: "support",
  onboarding: "support",
  subscriptions: "support",
};

// Pages without a sidebar entry of their own light the one they belong under.
const under: Record<string, string> = { tasks: "research" };

function initial(s: string) {
  return [...s][0]?.toUpperCase() ?? "?";
}

function OrgMark({ name, color }: { name: string; color: string }) {
  return (
    <span className="grid size-7 shrink-0 place-items-center rounded-md font-heading text-sm font-bold text-[#111]" style={{ background: color }}>
      {initial(name)}
    </span>
  );
}

// AppLayout is every page inside an organisation: the sidebar beside the page, or the
// page alone in the small window the bookmarklet opens.
export function AppLayout() {
  const { org } = useParams({ from: "/o/$org" });
  const nav = $api.useQuery("get", "/o/{org}/api/v1/nav", { params: { path: { org } } }, { refetchInterval: 60_000 });
  const popup = useMatch({ from: "/o/$org/add", shouldThrow: false })?.search.popup;
  const n = nav.data;

  useEffect(() => {
    if (n) document.title = popup ? `Add to ${n.org.name}` : `${n.org.name} · Prospector`;
  }, [n, popup]);
  useEffect(() => {
    // A session that ended while the page was open: sign in again and come back here.
    // A body that isn't JSON reaches here as a string.
    const failed: unknown = nav.error;
    if (typeof failed !== "object" || failed === null) return;
    if ("status" in failed && failed.status === 401) {
      window.location.assign("/login?next=" + encodeURIComponent(window.location.pathname + window.location.search));
    } else if (mustGoFirst(failed)) {
      // Loaded as a page, this address takes the browser where it has to go first.
      window.location.reload();
    }
  }, [nav.error]);

  if (nav.error && !n) return <p className="p-8 text-sm text-[var(--stop)]">{nav.error.detail ?? "This organisation couldn't be read. Reload to try again."}</p>;
  // Until the organisation has been read, the sidebar draws its outline and the page waits
  // beside it, so nothing already drawn moves when it comes.
  if (!n) {
    return popup ? null : (
      <div className="flex min-h-svh flex-wrap">
        <Sidebar org={org} />
        <main id="content" aria-busy className="relative min-w-0 flex-[999_1_640px] px-4 pb-16 pt-7 sm:px-8 lg:px-12" />
      </div>
    );
  }
  const session = { org, sales: n.sales, support: n.support, owner: n.owner };
  if (popup) {
    return (
      <SessionContext.Provider value={session}>
        <div className="min-h-svh px-5 py-5">
          <Outlet />
        </div>
      </SessionContext.Provider>
    );
  }
  return (
    <SessionContext.Provider value={session}>
      <div className="flex min-h-svh flex-wrap">
        <Sidebar org={org} n={n} />
        <main id="content" className="relative min-w-0 flex-[999_1_640px] px-4 pb-16 pt-7 sm:px-8 lg:px-12" style={{ "--org": n.org.color } as CSSProperties}>
          <Outlet />
        </main>
      </div>
    </SessionContext.Provider>
  );
}

function Sidebar({ org, n }: { org: string; n?: Nav }) {
  const signOut = $api.useMutation("delete", "/api/v1/session", { onSuccess: () => window.location.assign("/login") });
  const page = useLocation({ select: (l) => l.pathname.split("/")[3] ?? "" });
  const active = under[page] ?? page;
  const can = (area?: Area) => !area || !n || (area === "sales" ? n.sales : n.support);
  const item = (key: string, label: string, count = 0) => ({ key, label, count, view: !can(pageArea[key]), on: active === key });
  const counts = n?.counts;

  const items = [
    item("today", "Today", counts?.today),
    item("research", "Research", counts?.research),
    item("conversations", "Conversations", counts?.conversations),
    item("enquiries", "Enquiries", counts?.enquiries),
    item("onboarding", "Onboarding"),
    item("subscriptions", "Subscriptions"),
    item("campaigns", "Campaigns"),
    item("companies", "Companies", counts?.companies),
    item("settings", "Settings"),
    item("agents", "Agents & API"),
  ];

  return (
    <aside className="flex max-w-full flex-[1_1_240px] flex-col gap-4 night bg-background px-3.5 py-5 text-foreground md:sticky md:top-0 md:h-svh md:max-w-[260px]">
      <a href="/" className="flex items-center gap-2.5 px-1.5 font-heading text-lg font-bold tracking-tight">
        <img src="/assets/img/mark.svg" width="26" height="26" alt="" />
        Prospector.
      </a>
      {n ? <OrgSwitcher n={n} /> : <OrgOutline />}
      {!n ? (
        <span aria-hidden className="h-8 w-full animate-pulse rounded-lg bg-accent" />
      ) : (
        n.sales && (
          <Link to="/o/$org/add" params={{ org }} className={cn(button, primary, "w-full")}>
          + Add person
        </Link>
        )
      )}
      <nav className="flex flex-col gap-0.5" aria-label="Main">
        {items.map((i) => (
          <Link
            key={i.key}
            to={`/o/$org/${i.key}` as "/o/$org/today"}
            params={{ org }}
            aria-current={i.on ? "page" : undefined}
            className={cn(
              "flex items-center justify-between rounded-lg px-3 py-2 text-sm",
              i.on ? "bg-primary font-semibold text-primary-foreground" : "text-muted-foreground hover:bg-card hover:text-foreground",
            )}
          >
            <span>{i.label}</span>
            {i.view ? (
              <span className="rounded border border-border px-1 font-mono text-[11px] uppercase tracking-wider opacity-70" title="You can read this page, not change it">
                view
              </span>
            ) : (
              i.count > 0 && <span className="font-mono text-xs opacity-70">{i.count}</span>
            )}
          </Link>
        ))}
      </nav>
      <div className="mt-auto flex items-center gap-2.5 px-2.5 text-sm text-muted-foreground">
        <span className="grid size-6 place-items-center rounded-full bg-secondary text-xs text-foreground">{n && initial((n.user?.name ?? "") + (n.user?.email ?? ""))}</span>
        <span className="flex-1 truncate">
          <Text value={n && n.user?.name} w="9ch" />
          {n && n.role !== "owner" && <span className="block font-mono text-[11px]">{n.work}</span>}
        </span>
        <button type="button" className="text-xs hover:text-foreground" disabled={signOut.isPending} onClick={() => signOut.mutate({})}>
          Sign out
        </button>
      </div>
    </aside>
  );
}

// OrgOutline is the switcher's closed face before the organisation has been read.
function OrgOutline() {
  return (
    <div aria-hidden className="flex items-center gap-2.5 rounded-xl border border-border bg-card px-2.5 py-2">
      <span className="size-7 shrink-0 animate-pulse rounded-md bg-accent" />
      <span className="flex min-w-0 flex-1 flex-col leading-tight">
        <Bar w="10ch" className="text-sm" />
        <span className="font-mono text-xs text-muted-foreground">organisation</span>
      </span>
      <span className="size-3.5" />
    </div>
  );
}

function OrgSwitcher({ n }: { n: Nav }) {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const create = $api.useMutation("post", "/api/v1/orgs", {
    onSuccess: (o) => {
      setName("");
      navigate({ to: "/o/$org/settings", params: { org: o.slug } });
    },
  });
  return (
    <details className="group relative">
      <summary className="flex cursor-pointer list-none items-center gap-2.5 rounded-xl border border-border bg-card px-2.5 py-2">
        <OrgMark name={n.org.name} color={n.org.color} />
        <span className="flex min-w-0 flex-1 flex-col leading-tight">
          <span className="truncate text-sm font-semibold">{n.org.name}</span>
          <span className="font-mono text-xs text-muted-foreground">organisation</span>
        </span>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-muted-foreground" aria-hidden="true">
          <path d="M7 10l5-5 5 5M7 14l5 5 5-5"></path>
        </svg>
      </summary>
      <div className="absolute inset-x-0 top-full z-20 mt-2 flex flex-col gap-0.5 rounded-xl border border-border bg-card p-2 shadow-2xl">
        <p className="px-2 py-1 font-mono text-xs text-muted-foreground">{n.user?.email}</p>
        {n.orgs.map((o) => (
          // The server sends each organisation's address on to where its member starts.
          <a key={o.id} href={`/o/${o.slug}/`} className="flex items-center gap-3 rounded-lg px-2 py-2 text-sm text-foreground hover:bg-secondary">
            <OrgMark name={o.name} color={o.color} />
            <span className="flex-1">
              {o.name}
              <span className="block font-mono text-xs text-muted-foreground">{o.role}</span>
            </span>
            {o.id === n.org.id && (
              <span className="text-mark" aria-label="current">
                ✓
              </span>
            )}
          </a>
        ))}
        <form
          className="mt-1 flex gap-2 border-t border-border px-1 pt-2"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate({ body: { name } });
          }}
        >
          <label className="sr-only" htmlFor="new-org">
            New organisation
          </label>
          <input
            id="new-org"
            name="name"
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="New organisation"
            className="h-8 min-w-0 flex-1 rounded-md border border-input bg-muted px-2 text-sm"
          />
          <button type="submit" disabled={create.isPending} className={cn(button, outlineSm)}>
            Add
          </button>
        </form>
        {create.error && (
          <p className="px-2 text-xs text-[var(--stop)]" role="alert">
            {create.error.detail ?? "That didn't work. Try again."}
          </p>
        )}
      </div>
    </details>
  );
}

export function NotFound() {
  return (
    <div className="panel mx-auto mt-16 max-w-sm p-7">
      <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">404</span>
      <h1 className="font-heading text-2xl font-bold">
        Not found<span className="text-coral">.</span>
      </h1>
      <p className="text-sm text-muted-foreground">There's no such page here.</p>
    </div>
  );
}
