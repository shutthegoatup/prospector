// Prospector's own pieces, built on shadcn/ui. components/ui stays as the shadcn CLI
// writes it, so it can be updated; the look Prospector adds lives here.
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
