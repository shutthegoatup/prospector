// Prospector's own pieces, built on shadcn/ui. components/ui stays as the shadcn CLI
// writes it, so it can be updated; the look Prospector adds lives here.
import { useQueryClient } from "@tanstack/react-query";
import { type ComponentProps, type CSSProperties, type ReactNode, useEffect } from "react";
import { createLink, useParams } from "@tanstack/react-router";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import { $api } from "@/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import { Text } from "./skeleton";

// stop is the coral full stop after a page's title: a mark of Prospector's, the same in
// every language.
export const stop = ".";

export type Tone = "ok" | "warn" | "stop" | "agent" | "";

const tones: Record<Tone, string> = {
  ok: "text-[var(--ok)] bg-[color-mix(in_oklab,var(--ok)_12%,transparent)]",
  warn: "text-[var(--warn)] bg-[color-mix(in_oklab,var(--warn)_12%,transparent)]",
  stop: "text-[var(--stop)] bg-[color-mix(in_oklab,var(--stop)_12%,transparent)]",
  agent: "text-[var(--agent)] bg-[color-mix(in_oklab,var(--agent)_12%,transparent)]",
  "": "text-muted-foreground bg-secondary",
};

// Pill is a status: a dot and a few words, coloured by what it says.
// Meter is how far something has got towards a number: an agent's search towards what was asked.
export function Meter({ value, of }: { value: number; of: number }) {
  return (
    <Progress
      value={Math.min(100, (100 * value) / Math.max(of, 1))}
      aria-hidden
      className="h-2 min-w-[120px] flex-[1_1_160px] rounded bg-[#ece2cd] [&>[data-slot=progress-indicator]]:bg-[#5b45d6]"
    />
  );
}

export function Pill({ tone = "", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <Badge variant="secondary" className={cn("gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium", tones[tone])}>
      <span className="size-1.5 rounded-full bg-current" />
      {children}
    </Badge>
  );
}

// Section is one of a page's panels, with the ink edge every panel shares.
export function Section({ title, hint, children, className }: { title?: string; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <Card className={cn("panel gap-4 p-6 shadow-none", className)}>
      {(title || hint) && (
        <CardHeader className="gap-0.5 p-0">
          {title && <CardTitle className="font-heading text-lg font-semibold">{title}</CardTitle>}
          {hint && <CardDescription className="text-sm">{hint}</CardDescription>}
        </CardHeader>
      )}
      <CardContent className="flex flex-col gap-3 p-0">{children}</CardContent>
    </Card>
  );
}

// Eyebrow is the small mono capitals above a field or list.
export function Eyebrow({ children }: { children: ReactNode }) {
  return <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{children}</span>;
}

// Quote is someone else's words, exactly as they sent them.
export function Quote({ children }: { children: ReactNode }) {
  return <div className="whitespace-pre-wrap rounded-lg border-l-2 border-primary bg-muted px-4 py-3 text-[15px] leading-relaxed">{children}</div>;
}

export function Meta({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cn("font-mono text-xs text-muted-foreground", className)}>{children}</p>;
}

// Without a count yet, the chip keeps its place and its count pulses.
type ChipProps = Omit<ComponentProps<"a">, "children"> & { label: string; count?: number; on: boolean };

function Chip({ label, count, on, className, ref, ...props }: ChipProps) {
  return (
    <a
      ref={ref}
      {...props}
      className={cn(
        "inline-flex items-center gap-2 rounded-full border-[1.5px] px-3 py-1 text-sm no-underline",
        on ? "border-2 border-ink bg-primary font-semibold text-foreground shadow-[2px_2px_0_var(--ink)]" : "border-input bg-card text-foreground",
        className,
      )}
    >
      {label}
      <span className="font-mono text-xs opacity-70">
        <Text value={count} w="2ch" />
      </span>
    </a>
  );
}

// FilterChip is one of a list's filters, with how many it holds: a link to the same page
// with that filter in its address.
export const FilterChip = createLink(Chip);

// useRefresh reads everything the page shows again after a change, as a templ page reloads.
export function useRefresh() {
  const client = useQueryClient();
  return () => client.invalidateQueries();
}

const m = defineMessages({
  failed: {
    defaultMessage: "That didn't work. Reload and try again.",
    id: "B71vd+",
    description: "Shown when a change failed and the server said nothing about why",
  },
});

const v = {
  whose: defineMessage<{ area: string }>({
    defaultMessage: "An owner can add {area, select, sales {sales} other {support}} to you on the Team list in Settings.",
    id: "TUP8Mp",
    description: "After saying a page is work the member doesn't do: how they could be given it. {area} is the area of work, sales or support",
  }),
};

// Failed says why a change didn't happen, in the server's words.
export function Failed({ errors }: { errors: ({ detail?: string } | null | undefined)[] }) {
  const intl = useIntl();
  const error = errors.find(Boolean);
  if (!error) return null;
  return (
    <p className="w-full text-sm text-[var(--stop)]" role="alert">
      {error.detail ?? intl.formatMessage(m.failed)}
    </p>
  );
}

// Whose tells a member who reads a page, but whose work it isn't, why (said, a whole
// sentence of the page's own) and how to change that.
export function Whose({ area, said, can }: { area: "sales" | "support"; said: string; can: boolean }) {
  if (can) return null;
  return (
    <p className="mb-5 max-w-[70ch] border-t border-dashed border-border pt-2 font-mono text-xs text-mark">
      {said} <FormattedMessage {...v.whose} values={{ area }} />
    </p>
  );
}

// Empty is a list with nothing in it, saying what would be there.
export function Empty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="panel p-6 text-center">
      <p className="font-heading font-semibold">{title}</p>
      {hint && <p className="text-sm text-muted-foreground">{hint}</p>}
    </div>
  );
}

const titles = {
  page: defineMessage<{ page: string; org: string }>({
    defaultMessage: "{page} · {org} · Prospector",
    id: "vFbjH0",
    description: "The browser tab's title: the page, then the organisation it is in",
  }),
  org: defineMessage<{ org: string }>({
    defaultMessage: "{org} · Prospector",
    id: "97VVE5",
    description: "The browser tab's title while the page's own name is still loading",
  }),
};

// usePageTitle names the browser tab after the page and its organisation (WCAG 2.4.2);
// until the page knows its name, the organisation's alone.
export function usePageTitle(page: string | undefined) {
  const intl = useIntl();
  const { org } = useParams({ from: "/o/$org" });
  const name = $api.useQuery("get", "/o/{org}/api/v1/nav", { params: { path: { org } } }).data?.org.name;
  useEffect(() => {
    if (name) document.title = page ? intl.formatMessage(titles.page, { page, org: name }) : intl.formatMessage(titles.org, { org: name });
  }, [intl, page, name]);
}

// PageHead is a page's title, with the coral full stop, and anything beside it.
export function PageHead({ title, children }: { title: string; children?: ReactNode }) {
  usePageTitle(title);
  return (
    <header className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
      <h1 className="font-heading text-4xl font-bold tracking-tight">
        {title}
        <span className="text-coral">{stop}</span>
      </h1>
      {children}
    </header>
  );
}

// RailCard is a card with the offset shadow coloured by its state (ok, warn, stop),
// rising in after the index-th before it; a held one is greyed until hovered.
export function RailCard({ state, held, index, className, style, ...props }: ComponentProps<typeof Card> & { state: string; held?: boolean; index: number }) {
  return (
    <Card
      data-state={state}
      style={{ ...style, "--i": index } as CSSProperties}
      className={cn("rail rise gap-0 py-0", held && "opacity-45 grayscale transition-opacity hover:opacity-80", className)}
      {...props}
    />
  );
}

// ActionButton is a button in Prospector's ink-edged style.
export function ActionButton({ quiet, className, ...props }: ComponentProps<typeof Button> & { quiet?: boolean }) {
  return (
    <Button
      className={cn(
        "border-2 border-ink font-semibold text-foreground",
        quiet ? "bg-card hover:bg-muted" : "bg-primary shadow-[2px_2px_0_var(--ink)] hover:bg-primary",
        className,
      )}
      variant={quiet ? "outline" : "default"}
      {...props}
    />
  );
}
