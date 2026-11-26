import type { ReactNode } from "react";
import { ActionButton } from "@/components/app";

// Quote is someone's words, kept as they wrote them.
export function Quote({ children }: { children: ReactNode }) {
  return <div className="whitespace-pre-wrap rounded-lg border-l-2 border-primary bg-muted px-4 py-3 text-[15px] leading-relaxed">{children}</div>;
}

// Pager walks a list a cursor at a time: back to its first page, or on to the next.
export function Pager({ cursor, next, go }: { cursor?: string; next?: string; go: (cursor?: string) => void }) {
  if (!cursor && !next) return null;
  return (
    <nav className="mt-3 flex justify-between gap-3" aria-label="Pages">
      <span>
        {cursor && (
          <ActionButton quiet size="sm" onClick={() => go(undefined)}>
            ← First
          </ActionButton>
        )}
      </span>
      <span>
        {next && (
          <ActionButton quiet size="sm" onClick={() => go(next)}>
            Older →
          </ActionButton>
        )}
      </span>
    </nav>
  );
}
