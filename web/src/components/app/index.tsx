// Prospector's own pieces, built on shadcn/ui. components/ui stays as the shadcn CLI
// writes it, so it can be updated; the look Prospector adds lives here.
import { useQueryClient } from "@tanstack/react-query";
import type { ComponentProps, ReactNode } from "react";
import { Link } from "react-router";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export type Tone = "ok" | "warn" | "stop" | "agent" | "";

const tones: Record<Tone, string> = {
  ok: "text-[var(--ok)] bg-[color-mix(in_oklab,var(--ok)_12%,transparent)]",
  warn: "text-[var(--warn)] bg-[color-mix(in_oklab,var(--warn)_12%,transparent)]",
  stop: "text-[var(--stop)] bg-[color-mix(in_oklab,var(--stop)_12%,transparent)]",
  agent: "text-[var(--agent)] bg-[color-mix(in_oklab,var(--agent)_12%,transparent)]",
  "": "text-muted-foreground bg-secondary",
};

// Pill is a status: a dot and a few words, coloured by what it says.
export function Pill({ tone = "", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <Badge variant="secondary" className={cn("gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium", tones[tone])}>
      <span className="size-1.5 rounded-full bg-current" />
      {children}
    </Badge>
  );
}

// Section is one of a page's panels, with the ink edge every panel shares.
export function Section({ title, hint, children, className }: { title?: string; hint?: string; children: ReactNode; className?: string }) {
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

// FilterChip is one of a list's filters, with how many it holds.
export function FilterChip({ to, label, count, on }: { to: string; label: string; count: number; on: boolean }) {
  return (
    <Link
      to={to}
      className={cn(
        "inline-flex items-center gap-2 rounded-full border-[1.5px] px-3 py-1 text-sm no-underline",
        on ? "border-2 border-ink bg-primary font-semibold text-foreground shadow-[2px_2px_0_var(--ink)]" : "border-input bg-card text-foreground",
      )}
    >
      {label}
      <span className="font-mono text-xs opacity-70">{count}</span>
    </Link>
  );
}

// useRefresh reads everything the page shows again after a change, as a templ page reloads.
export function useRefresh() {
  const client = useQueryClient();
  return () => client.invalidateQueries();
}

// Failed says why a change didn't happen, in the server's words.
export function Failed({ errors }: { errors: ({ detail?: string } | null | undefined)[] }) {
  const error = errors.find(Boolean);
  if (!error) return null;
  return (
    <p className="w-full text-sm text-[var(--stop)]" role="alert">
      {error.detail ?? "That didn't work. Reload and try again."}
    </p>
  );
}

// Whose tells a member who reads a page, but whose work it isn't, why and how to change that.
export function Whose({ area, work, can }: { area: "sales" | "support"; work: string; can: boolean }) {
  if (can) return null;
  return (
    <p className="mb-5 max-w-[70ch] border-t border-dashed border-border pt-2 font-mono text-xs text-mark">
      {work} {area} work, so here you read it. An owner can add {area} to you on the Team list in Settings.
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

// PageHead is a page's title, with the coral full stop, and anything beside it.
export function PageHead({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <header className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
      <h1 className="font-heading text-4xl font-bold tracking-tight">
        {title}
        <span className="text-coral">.</span>
      </h1>
      {children}
    </header>
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
