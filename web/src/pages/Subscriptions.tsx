import { Link } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { Empty, Failed, PageHead, Pill, type Tone, Whose } from "@/components/app";
import { standingText, SubscriptionUpdate, termText } from "@/components/app/subscription";
import { Bar, Placeholders, placeholders, Text } from "@/components/app/skeleton";
import { useSession } from "@/session";

type Subscription = components["schemas"]["Subscription"];

function tone(s: Subscription): Tone {
  if (s.status === "active") return "ok";
  if (s.status === "notice given" || s.status === "ending") return "warn";
  return "";
}

export function SubscriptionsPage() {
  const { org, support } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/subscriptions", { params: { path: { org }, query: { limit: 200 } } });
  const worth = list.data?.contracted;
  return (
    <>
      <PageHead title="Subscriptions">
        {(worth || (!list.data && !list.error)) && (
          <span className="font-mono text-xs text-muted-foreground">
            <Text value={worth && `${worth} a year at today's prices`} w="26ch" />
          </span>
        )}
      </PageHead>
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        What each customer is on, and until when. The one that needs someone soonest comes first: a day to cancel by, a renewal or an end.
        Record one from a company's page. Prospector notes what was agreed and when it's due; it takes no payment and sends no invoice.
      </p>
      <Whose area="support" work="Subscriptions are" can={support} />
      <Failed errors={[list.error]} />
      {list.data && list.data.items.length === 0 && <Empty title="No subscriptions yet" hint="Open a company and record what they are on." />}
      {!list.data && !list.error && (
        <Placeholders className="overflow-hidden panel">
          {Array.from({ length: placeholders }, (_, i) => (
            <SubscriptionLine key={i} />
          ))}
        </Placeholders>
      )}
      {list.data && list.data.items.length > 0 && (
        <div className="overflow-hidden panel">
          {list.data.items.map((s) => (
            <SubscriptionLine key={s.id} s={s} />
          ))}
        </div>
      )}
    </>
  );
}

// SubscriptionLine is one subscription, or its outline while the list loads.
function SubscriptionLine({ s }: { s?: Subscription }) {
  const { org, support } = useSession();
  if (!s) {
    return (
      <div className="border-b border-border px-4 py-3 text-sm last:border-0">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="min-w-0">
            <b>
              <Bar w="14ch" />
            </b>
            <span className="text-muted-foreground"> · </span>
            <Bar w="10ch" />
            <span className="block font-mono text-xs">
              <Bar w="30ch" />
            </span>
          </span>
          <Pill>
            <Bar w="6ch" />
          </Pill>
        </div>
        {support && (
          <p className="mt-1 font-mono text-xs">
            <Bar w="6ch" />
          </p>
        )}
      </div>
    );
  }
  const price = s.figure ? `${s.figure} a ${s.per}` : "";
  return (
    <div className="border-b border-border px-4 py-3 text-sm last:border-0">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="min-w-0">
          <Link to="/o/$org/companies/$id" params={{ org, id: s.company_id }} className="font-bold text-foreground hover:text-mark">
            {s.company}
          </Link>
          <span className="text-muted-foreground"> · {s.name}</span>
          <span className="block font-mono text-xs text-muted-foreground">
            {[price, termText(s), standingText(s)].filter((p) => p.trim()).join(" · ")}
          </span>
        </span>
        <span className="flex items-center gap-2">
          {s.soon && <Pill tone="warn">within 30 days</Pill>}
          <Pill tone={tone(s)}>{s.status}</Pill>
        </span>
      </div>
      {support && <SubscriptionUpdate s={s} />}
    </div>
  );
}

