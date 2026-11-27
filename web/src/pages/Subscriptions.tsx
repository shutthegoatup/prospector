import { Link } from "@tanstack/react-router";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { Empty, Failed, PageHead, Pill, type Tone, Whose } from "@/components/app";
import { standingText, statusLabel, SubscriptionUpdate, termText } from "@/components/app/subscription";
import { Bar, Placeholders, placeholders, Text } from "@/components/app/skeleton";
import { useSession } from "@/session";

type Subscription = components["schemas"]["Subscription"];

const m = defineMessages({
  title: { defaultMessage: "Subscriptions", id: "MzTbJx", description: "Title of the page of customers' subscriptions" },
  whose: {
    defaultMessage: "Subscriptions are support work, so here you read it.",
    id: "lA8Isj",
    description: "For a member who doesn't do support work, on the subscriptions page",
  },
  none: { defaultMessage: "No subscriptions yet", id: "sMvOY0", description: "When no subscription has been recorded" },
  noneHint: { defaultMessage: "Open a company and record what they are on.", id: "Ct7x4z", description: "Under 'No subscriptions yet'" },
  soon: { defaultMessage: "within 30 days", id: "boDw4Q", description: "Tag on a subscription that needs someone within 30 days" },
});

const v = {
  worth: defineMessage<{ worth: string }>({
    defaultMessage: "{worth} a year at today's prices",
    id: "aibbmm",
    description: "Beside the title: what every subscription comes to in a year. {worth} is an amount of money",
  }),
  price: defineMessage<{ figure: string; per: string }>({
    defaultMessage: "{figure} {per, select, month {a month} other {a year}}",
    id: "iIO9P4",
    description: "A subscription's price. {figure} is an amount of money",
  }),
};

// Between the parts of a subscription's line: a mark, not a word.
const joiner = " · ";

function tone(s: Subscription): Tone {
  if (s.status === "active") return "ok";
  if (s.status === "notice given" || s.status === "ending") return "warn";
  return "";
}

export function SubscriptionsPage() {
  const say = useIntl().formatMessage;
  const { org, support } = useSession();
  const list = $api.useQuery("get", "/o/{org}/api/v1/subscriptions", { params: { path: { org }, query: { limit: 200 } } });
  const worth = list.data?.contracted;
  return (
    <>
      <PageHead title={say(m.title)}>
        {(worth || (!list.data && !list.error)) && (
          <span className="font-mono text-xs text-muted-foreground">
            <Text value={worth && say(v.worth, { worth })} w="26ch" />
          </span>
        )}
      </PageHead>
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        <FormattedMessage
          defaultMessage="What each customer is on, and until when. The one that needs someone soonest comes first: a day to cancel by, a renewal or an end. Record one from a company's page. {prospector} notes what was agreed and when it's due; it takes no payment and sends no invoice."
          id="RYVKBb"
          description="What the subscriptions page is, under its title. {prospector} is the app's name"
          values={{ prospector: "Prospector" }}
        />
      </p>
      <Whose area="support" said={say(m.whose)} can={support} />
      <Failed errors={[list.error]} />
      {list.data && list.data.items.length === 0 && <Empty title={say(m.none)} hint={say(m.noneHint)} />}
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
  const say = useIntl().formatMessage;
  const { org, support } = useSession();
  if (!s) {
    return (
      <div className="border-b border-border px-4 py-3 text-sm last:border-0">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="min-w-0">
            <b>
              <Bar w="14ch" />
            </b>
            <span className="text-muted-foreground">{joiner}</span>
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
  const price = s.figure ? say(v.price, { figure: s.figure, per: s.per }) : "";
  return (
    <div className="border-b border-border px-4 py-3 text-sm last:border-0">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="min-w-0">
          <Link to="/o/$org/companies/$id" params={{ org, id: s.company_id }} className="font-bold text-foreground hover:text-mark">
            {s.company}
          </Link>
          <span className="text-muted-foreground">
            {joiner}
            {s.name}
          </span>
          <span className="block font-mono text-xs text-muted-foreground">{[price, termText(s), standingText(s)].filter((p) => p.trim()).join(joiner)}</span>
        </span>
        <span className="flex items-center gap-2">
          {s.soon && <Pill tone="warn">{say(m.soon)}</Pill>}
          <Pill tone={tone(s)}>{statusLabel(s.status)}</Pill>
        </span>
      </div>
      {support && <SubscriptionUpdate s={s} />}
    </div>
  );
}
