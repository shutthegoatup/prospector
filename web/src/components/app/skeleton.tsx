import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A component's loading state is the same component drawn without its data: each piece
// of text it would show is a Bar in its place, one line tall, so nothing moves when the
// data arrives.

// Bar stands in for a line of text as wide as w, in the line box the text will fill.
export function Bar({ w, className }: { w: string; className?: string }) {
  return (
    <span aria-hidden className={cn("inline-block h-[1lh] max-w-full py-[0.25lh] align-top", className)} style={{ width: w }}>
      <span className="block h-full animate-pulse rounded-md bg-accent" />
    </span>
  );
}

// Text is a value once it has come, and a Bar of about its width until then.
export function Text({ value, w, className }: { value: ReactNode | undefined; w: string; className?: string }) {
  return value === undefined ? <Bar w={w} className={className} /> : <>{value}</>;
}

// Loading marks a region whose data hasn't come, for assistive technology.
export function Loading({ busy, children, className }: { busy: boolean; children: ReactNode; className?: string }) {
  return (
    <div aria-busy={busy || undefined} className={className}>
      {children}
    </div>
  );
}

// placeholders is how many rows a list shows before it knows its length: about a
// screen's worth, so whatever follows the list starts below the fold either way.
export const placeholders = 10;
