import { useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Failed, Meta, PageHead } from "@/components/app";
import { Input } from "@/components/ui/input";
import { plural } from "@/lib/format";
import { cn } from "@/lib/utils";
import { pipelines } from "@/search";
import { useSession } from "@/session";

type Pipeline = components["schemas"]["Pipeline"];

const stages: readonly Pipeline[] = pipelines;
const pageSize = 50;

// StageTag is where a company stands, lit once someone there has answered.
export function StageTag({ stage }: { stage: string }) {
  const answered = ["replied", "meeting", "proposal", "won"].includes(stage);
  return (
    <span
      className={cn(
        "whitespace-nowrap rounded-md px-2 py-0.5 font-mono text-[11px] uppercase tracking-wide",
        answered ? "bg-[color-mix(in_oklab,var(--primary)_14%,transparent)] text-mark" : "bg-secondary text-muted-foreground",
      )}
    >
      {stage}
    </span>
  );
}

// usePages walks one list a page at a time by its cursors, starting again whenever
// what the list is of (its filters) changes.
export function usePages(of: string) {
  const [at, setAt] = useState<{ of: string; cursors: string[] }>({ of, cursors: [] });
  const cursors = at.of === of ? at.cursors : [];
  return {
    cursor: cursors[cursors.length - 1],
    n: cursors.length + 1,
    next: (cursor: string) => setAt({ of, cursors: [...cursors, cursor] }),
    previous: () => setAt({ of, cursors: cursors.slice(0, -1) }),
  };
}

export function Pager({ n, total, size, next, onNext, onPrevious }: { n: number; total: number; size: number; next?: string; onNext: (cursor: string) => void; onPrevious: () => void }) {
  const pages = Math.max(1, Math.ceil(total / size));
  // Past the first page, Previous stays: a page emptied by a change shouldn't strand anyone.
  if (pages <= 1 && n <= 1) return null;
  return (
    <nav className="mt-3 flex flex-wrap items-center justify-between gap-3" aria-label="Pages">
      <span>
        {n > 1 && (
          <ActionButton quiet size="sm" onClick={onPrevious}>
            ← Previous
          </ActionButton>
        )}
      </span>
      <Meta>
        Page {Math.min(n, pages)} of {pages}
      </Meta>
      <span>
        {next && (
          <ActionButton quiet size="sm" onClick={() => onNext(next)}>
            Next →
          </ActionButton>
        )}
      </span>
    </nav>
  );
}

export function CompaniesPage() {
  const { org } = useSession();
  const search = useSearch({ from: "/o/$org/companies" });
  const { q = "", segment = "", stage } = search;
  const navigate = useNavigate();
  const filters = JSON.stringify(search);
  const pages = usePages(filters);
  const list = $api.useQuery("get", "/o/{org}/api/v1/companies", {
    params: { path: { org }, query: { q: q || undefined, segment: segment || undefined, stage, limit: pageSize, cursor: pages.cursor } },
  });
  const segments = $api.useQuery("get", "/o/{org}/api/v1/segments", { params: { path: { org }, query: { limit: 200 } } });

  return (
    <>
      <PageHead title="Companies">
        {list.data && <span className="font-mono text-xs text-muted-foreground">{plural(list.data.total, "company", "companies")}</span>}
      </PageHead>
      <form
        key={filters}
        className="mb-5 flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const form = new FormData(e.currentTarget);
          const value = (k: string) => (form.get(k) as string | null) || undefined;
          navigate({ to: "/o/$org/companies", params: { org }, search: { q: value("q"), segment: value("segment"), stage: pipelines.find((p) => p === value("stage")) } });
        }}
      >
        <label className="sr-only" htmlFor="q">
          Search
        </label>
        <Input
          id="q"
          type="search"
          name="q"
          defaultValue={q}
          placeholder="Search companies, people, notes"
          className="h-9 min-w-[220px] flex-1 rounded-lg border-input bg-muted px-3 text-sm"
        />
        <label className="sr-only" htmlFor="segment">
          Segment
        </label>
        {/* Keyed on the segments arriving, so the one in the address is picked once it is an option. */}
        <select key={segments.data ? "segments" : "loading"} id="segment" name="segment" defaultValue={segment} className="h-9 rounded-lg border border-input bg-muted px-2 text-sm">
          <option value="">All segments</option>
          {(segments.data?.items ?? []).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label className="sr-only" htmlFor="stage">
          Stage
        </label>
        <select id="stage" name="stage" defaultValue={stage ?? ""} className="h-9 rounded-lg border border-input bg-muted px-2 text-sm">
          <option value="">Any stage</option>
          {stages.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <ActionButton quiet size="sm" type="submit" className="h-9">
          Filter
        </ActionButton>
      </form>
      <Failed errors={[list.error, segments.error]} />
      {list.data && list.data.items.length === 0 && <Empty title="No companies match" hint="Clear the filters, or add companies through Research or an agent." />}
      {list.data && list.data.items.length > 0 && (
        <div className="overflow-hidden panel">
          {list.data.items.map((r) => (
            <Link
              key={r.id}
              to="/o/$org/companies/$id"
              params={{ org, id: r.id }}
              className="flex items-center justify-between gap-3 border-b border-border px-4 py-3 text-sm text-foreground no-underline last:border-0 hover:bg-secondary hover:text-foreground"
            >
              <span className="min-w-0">
                <b>{r.name}</b>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {[r.country, r.segment, plural(r.people, "person", "people")].filter((p) => p.trim()).join(" · ")}
                </span>
              </span>
              <span className="flex items-center gap-2">
                {r.grade && <span className="font-mono text-xs text-mark">{r.grade}</span>}
                <StageTag stage={r.pipeline} />
              </span>
            </Link>
          ))}
        </div>
      )}
      {list.data && (
        <Pager n={pages.n} total={list.data.total} size={pageSize} next={list.data.next_cursor} onNext={pages.next} onPrevious={pages.previous} />
      )}
    </>
  );
}
