import { Link } from "@tanstack/react-router";
import { type ReactNode, useEffect } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, PageHead, Pill, type Tone, useRefresh, Whose } from "@/components/app";
import { Bar, Loading } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useSession } from "@/session";

type Card = components["schemas"]["DealCard"];
type DealChange = components["schemas"]["DealChange"];

const stages = [
  { key: "replied", label: "Replied" },
  { key: "meeting", label: "Meeting" },
  { key: "proposal", label: "Proposal" },
  { key: "won", label: "Won" },
  { key: "lost", label: "Lost" },
];

const kinds = [
  { key: "new", label: "New business" },
  { key: "renewal", label: "Renewal" },
  { key: "expansion", label: "Expansion" },
];

const select = "h-8 rounded-lg border border-input bg-muted px-2 text-sm";
const signs: Record<string, string> = { GBP: "£", EUR: "€", USD: "$" };

function today() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// money writes an amount in hundredths as the server does: "£14,000", "CHF 999.50".
function money(amount: number, currency: string) {
  let whole = Math.trunc(amount / 100).toString();
  for (let i = whole.length - 3; i > 0; i -= 3) whole = whole.slice(0, i) + "," + whole.slice(i);
  const cents = amount % 100;
  if (cents !== 0) whole += "." + String(cents).padStart(2, "0");
  return currency in signs ? signs[currency] + whole : `${currency} ${whole}`.trim();
}

function worth(cards: Card[]) {
  const sums = new Map<string, number>();
  for (const c of cards) if (c.amount !== 0) sums.set(c.currency, (sums.get(c.currency) ?? 0) + c.amount);
  return [...sums].map(([currency, sum]) => money(sum, currency)).sort().join(" + ");
}

function kindLabel(kind: string) {
  return kinds.find((k) => k.key === kind)?.label ?? kind;
}

function dealMeta(c: Card, withKind: boolean) {
  return [withKind && c.kind !== "new" ? kindLabel(c.kind) : "", c.close_date && "closes " + c.close_date, c.owner].filter(Boolean).join(" · ");
}

function needsNextStep(c: Card, day: string) {
  if (c.stage === "won" || c.stage === "lost") return false;
  return c.next_step === "" || (c.next_date !== "" && c.next_date <= day);
}

function edge(c: Card, day: string) {
  if (needsNextStep(c, day)) return "border-[color-mix(in_oklab,var(--warn)_55%,transparent)]";
  if (c.stage === "lost") return "border-border opacity-60";
  return "border-border";
}

function nextText(c: Card, day: string): [string, Tone] {
  if (c.stage === "won" || c.stage === "lost") return ["", ""];
  if (c.next_step === "") return ["No next step yet", "warn"];
  if (c.next_date && c.next_date < day) return [`${c.next_step} · overdue since ${c.next_date}`, "stop"];
  if (c.next_date) return [`${c.next_step} · ${c.next_date}`, "agent"];
  return [c.next_step, "agent"];
}

// useBoard reads the whole board, page after page: a stage's count and worth are of all its deals.
function useBoard() {
  const { org } = useSession();
  const board = $api.useInfiniteQuery(
    "get",
    "/o/{org}/api/v1/conversations",
    { params: { path: { org }, query: { limit: 200 } } },
    { initialPageParam: "", getNextPageParam: (last) => last.next_cursor || undefined },
  );
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = board;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  const pages = board.data?.pages ?? [];
  return { cards: pages.flatMap((p) => p.items), planned: pages[0]?.onboarding_steps ?? 0, loading: !board.data && !board.error, error: board.error };
}

export function ConversationsPage() {
  const { org, sales } = useSession();
  const { cards, planned, loading, error } = useBoard();
  const members = $api.useQuery("get", "/o/{org}/api/v1/members", { params: { path: { org }, query: { limit: 200 } } });
  const names = (members.data?.items ?? []).map((m) => m.name);
  const day = today();
  return (
    <>
      <PageHead title="Conversations" />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        Everyone who replied, until it's won or lost. Each needs a next step with a date; ones without, or due today, also show on Today. Give one a
        value and each stage shows what it is worth. A renewal or an expansion with someone already here is a deal of its own, started from their
        card.
      </p>
      <Whose area="sales" work="Deals are" can={sales} />
      <Failed errors={[error, members.error]} />
      <div className="overflow-x-auto pb-4">
        <Loading busy={loading} className="flex min-w-max gap-4">
          {stages.map((st) => {
            const here = cards.filter((c) => c.stage === st.key);
            const sum = worth(here);
            return (
              <section key={st.key} className="flex w-[280px] flex-col gap-2.5" aria-label={st.label}>
                <div className="flex items-baseline justify-between border-b-2 border-border px-1 pb-1.5">
                  <h2 className="font-heading text-base font-semibold">{st.label}</h2>
                  <span className="font-mono text-xs text-muted-foreground">
                    {loading ? (
                      <Bar w="2ch" />
                    ) : (
                      <>
                        {here.length}
                        {sum && " · " + sum}
                      </>
                    )}
                  </span>
                </div>
                {loading && Array.from({ length: 3 }, (_, i) => <DealCard key={i} />)}
                {!loading && here.length === 0 && <p className="rounded-xl border border-dashed border-border p-3.5 font-mono text-xs text-muted-foreground">None</p>}
                {here.map((c) =>
                  c.seq === 0 ? (
                    <DealCard key={"c" + c.contact_id} c={c} members={names} planned={planned} day={day} />
                  ) : (
                    <OtherDealCard key={"d" + c.id} c={c} members={names} planned={planned} day={day} />
                  ),
                )}
              </section>
            );
          })}
        </Loading>
      </div>
    </>
  );
}

function CompanyLink({ c }: { c: Card }) {
  const { org } = useSession();
  if (!c.company_id) return null;
  return (
    <Link to="/o/$org/companies/$id" params={{ org, id: c.company_id }} className="font-mono text-xs text-muted-foreground hover:text-mark">
      {" · " + c.company}
    </Link>
  );
}

function Value({ c, meta }: { c: Card; meta: string }) {
  if (!c.value && !meta) return null;
  return (
    <p className="font-mono text-xs text-muted-foreground">
      {c.value && (
        <>
          <b className="text-foreground">{c.value}</b>{" "}
        </>
      )}
      {meta}
    </p>
  );
}

function Next({ c, day }: { c: Card; day: string }) {
  const [text, tone] = nextText(c, day);
  if (!text) return null;
  return (
    <div>
      <Pill tone={tone}>{text}</Pill>
    </div>
  );
}

// WonNext offers to start onboarding the company a won deal is with, or says it has begun.
function WonNext({ c, planned, day }: { c: Card; planned: number; day: string }) {
  const { org, sales, support } = useSession();
  const refresh = useRefresh();
  const start = $api.useMutation("post", "/o/{org}/api/v1/onboardings", { onSuccess: refresh });
  if (c.stage !== "won" || !c.company_id) return null;
  if (c.onboarding) {
    return (
      <Link to="/o/$org/companies/$id" params={{ org, id: c.company_id }} className="font-mono text-xs text-mark underline">
        {c.onboarding === "running" ? "Onboarding under way" : "Onboarded"}
      </Link>
    );
  }
  if (planned === 0 || !(sales || support)) return null;
  return (
    <div>
      <ActionButton
        size="sm"
        disabled={start.isPending}
        onClick={() => start.mutate({ params: { path: { org } }, body: { company: c.company_id, start: day } })}
      >
        Start onboarding
      </ActionButton>
      <Failed errors={[start.error]} />
    </div>
  );
}

function Field({ label, children, row }: { label: string; children: ReactNode; row?: boolean }) {
  return (
    <label className={row ? "flex items-center gap-2" : "flex flex-col gap-1"}>
      <Eyebrow>{label}</Eyebrow>
      {children}
    </label>
  );
}

// Text saves when it is left changed, as a field on the server's pages does.
function Text({ value, save, placeholder, type }: { value: string; save: (v: string) => void; placeholder?: string; type?: "date" }) {
  if (type === "date") {
    return <Input key={value} type="date" defaultValue={value} className="h-8 bg-muted" onChange={(e) => save(e.target.value)} />;
  }
  return (
    <Input
      key={value}
      defaultValue={value}
      placeholder={placeholder}
      className="h-8 bg-muted"
      onBlur={(e) => e.target.value !== value && save(e.target.value)}
      onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
    />
  );
}

function Choice({ value, save, children, label }: { value: string; save: (v: string) => void; children: ReactNode; label?: string }) {
  return (
    <select key={value} aria-label={label} defaultValue={value} onChange={(e) => save(e.target.value)} className={select}>
      {children}
    </select>
  );
}

// DealFields are a deal's value, close date, kind and owner.
function DealFields({ c, members, save }: { c: Card; members: string[]; save: (change: DealChange) => void }) {
  const left = c.owner !== "" && !members.includes(c.owner);
  return (
    <>
      <Field label="Value">
        <Text value={c.value} placeholder="e.g. 14,000" save={(amount) => save({ amount })} />
      </Field>
      <Field label="Expected to close">
        <Text type="date" value={c.close_date} save={(close_date) => save({ close_date })} />
      </Field>
      <Field label="Kind" row>
        <Choice value={c.kind || "new"} save={(kind) => save({ kind })}>
          {kinds.map((k) => (
            <option key={k.key} value={k.key}>
              {k.label}
            </option>
          ))}
        </Choice>
      </Field>
      <Field label="Owner" row>
        <Choice value={c.owner} save={(owner) => save({ owner })}>
          <option value="">Nobody yet</option>
          {left && <option value={c.owner}>{c.owner + " (left)"}</option>}
          {members.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </Choice>
      </Field>
    </>
  );
}

function StageChoice({ value, save }: { value: string; save: (v: string) => void }) {
  return (
    <Field label="Stage" row>
      <Choice value={value} save={save}>
        {stages.map((s) => (
          <option key={s.key} value={s.key}>
            {s.label}
          </option>
        ))}
      </Choice>
    </Field>
  );
}

type CardProps = { c: Card; members: string[]; planned: number; day: string };

// DealCard is someone in conversation, or their card's outline while the board loads.
function DealCard({ c, members = [], planned = 0, day = "" }: Partial<CardProps>) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const contact = $api.useMutation("patch", "/o/{org}/api/v1/contacts/{id}", { onSuccess: refresh });
  const deal = $api.useMutation("patch", "/o/{org}/api/v1/contacts/{id}/deal", { onSuccess: refresh });
  const another = $api.useMutation("post", "/o/{org}/api/v1/deals", { onSuccess: refresh });
  if (!c) {
    return (
      <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
        <div>
          <b>
            <Bar w="10ch" />
          </b>
          <span className="font-mono text-xs"> <Bar w="12ch" /></span>
        </div>
        {sales && (
          <p className="font-mono text-xs">
            <Bar w="7ch" />
          </p>
        )}
      </article>
    );
  }
  const path = { org, id: c.contact_id };
  const person = (body: components["schemas"]["ContactChange"]) => contact.mutate({ params: { path }, body });
  return (
    <article className={cn("flex flex-col gap-2.5 rounded-xl border bg-card p-3.5 text-sm", edge(c, day))}>
      <div>
        <b>{c.person}</b>
        <CompanyLink c={c} />
      </div>
      {c.call_notes && <p className="line-clamp-3 text-[13px] text-muted-foreground">{c.call_notes}</p>}
      <Value c={c} meta={dealMeta(c, true)} />
      <Next c={c} day={day} />
      <WonNext c={c} planned={planned} day={day} />
      {sales && (
        <details className="text-sm">
          <summary className="cursor-pointer font-mono text-xs text-muted-foreground">Update</summary>
          <div className="mt-2 flex flex-col gap-2">
            <StageChoice value={c.stage || "replied"} save={(stage) => person({ stage })} />
            <Field label="Next step">
              <Text value={c.next_step} placeholder="e.g. send recap" save={(next_step) => person({ next_step })} />
            </Field>
            <Field label="By">
              <Text type="date" value={c.next_date} save={(next_date) => person({ next_date })} />
            </Field>
            <DealFields c={c} members={members} save={(body) => deal.mutate({ params: { path }, body })} />
            {c.stage === "lost" && (
              <Field label="Why lost">
                <Text value={c.lost_reason} save={(lost_reason) => person({ lost_reason })} />
              </Field>
            )}
            <form
              className="flex items-center gap-2 border-t border-border pt-2"
              onSubmit={(e) => {
                e.preventDefault();
                const kind = new FormData(e.currentTarget).get("kind") as string;
                another.mutate({ params: { path: { org } }, body: { contact: c.contact_id, kind } });
              }}
            >
              <Eyebrow>Another deal</Eyebrow>
              <select name="kind" aria-label="Kind of deal" defaultValue="renewal" className={select}>
                {kinds.map((k) => (
                  <option key={k.key} value={k.key}>
                    {k.label}
                  </option>
                ))}
              </select>
              <ActionButton quiet size="sm" type="submit" disabled={another.isPending}>
                Start
              </ActionButton>
            </form>
          </div>
        </details>
      )}
      <Failed errors={[contact.error, deal.error, another.error]} />
    </article>
  );
}

// OtherDealCard is a renewal, an expansion or a further deal with someone in conversation.
function OtherDealCard({ c, members, planned, day }: CardProps) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const change = $api.useMutation("patch", "/o/{org}/api/v1/deals/{id}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/deals/{id}", { onSuccess: refresh });
  const path = { org, id: c.id };
  const save = (body: DealChange) => change.mutate({ params: { path }, body });
  return (
    <article className={cn("flex flex-col gap-2.5 rounded-xl border bg-card p-3.5 text-sm", edge(c, day))}>
      <div>
        <b>{kindLabel(c.kind)}</b>
        <span className="text-muted-foreground">{" with " + c.person}</span>
        <CompanyLink c={c} />
      </div>
      <Value c={c} meta={dealMeta(c, false)} />
      <Next c={c} day={day} />
      <WonNext c={c} planned={planned} day={day} />
      {sales && (
        <details className="text-sm">
          <summary className="cursor-pointer font-mono text-xs text-muted-foreground">Update</summary>
          <div className="mt-2 flex flex-col gap-2">
            <StageChoice value={c.stage} save={(stage) => save({ stage })} />
            <Field label="Next step">
              <Text value={c.next_step} placeholder="e.g. send renewal terms" save={(next_step) => save({ next_step })} />
            </Field>
            <Field label="By">
              <Text type="date" value={c.next_date} save={(next_date) => save({ next_date })} />
            </Field>
            <DealFields c={c} members={members} save={save} />
            {c.stage === "lost" && (
              <Field label="Why lost">
                <Text value={c.lost_reason} save={(lost_reason) => save({ lost_reason })} />
              </Field>
            )}
            <div className="flex items-center gap-2 border-t border-border pt-2">
              <ActionButton quiet size="sm" disabled={remove.isPending} onClick={() => remove.mutate({ params: { path } })}>
                Remove this deal
              </ActionButton>
            </div>
          </div>
        </details>
      )}
      <Failed errors={[change.error, remove.error]} />
    </article>
  );
}
