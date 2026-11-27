import type { ReactNode } from "react";
import { defineMessages, useIntl } from "react-intl";
import { ActionButton } from "@/components/app";

const m = defineMessages({
  pages: { defaultMessage: "Pages", id: "aSiD4J", description: "What a screen reader calls the links between pages of a long list" },
  first: { defaultMessage: "← First", id: "CgAAU4", description: "Button back to the first page of a list" },
  older: { defaultMessage: "Older →", id: "IlooCq", description: "Button on to the next page of a list, of older things" },
});

// Quote is someone's words, kept as they wrote them.
export function Quote({ children }: { children: ReactNode }) {
  return <div className="whitespace-pre-wrap rounded-lg border-l-2 border-primary bg-muted px-4 py-3 text-[15px] leading-relaxed">{children}</div>;
}

// Pager walks a list a cursor at a time: back to its first page, or on to the next.
export function Pager({ cursor, next, go }: { cursor?: string; next?: string; go: (cursor?: string) => void }) {
  const intl = useIntl();
  if (!cursor && !next) return null;
  return (
    <nav className="mt-3 flex justify-between gap-3" aria-label={intl.formatMessage(m.pages)}>
      <span>
        {cursor && (
          <ActionButton quiet size="sm" onClick={() => go(undefined)}>
            {intl.formatMessage(m.first)}
          </ActionButton>
        )}
      </span>
      <span>
        {next && (
          <ActionButton quiet size="sm" onClick={() => go(next)}>
            {intl.formatMessage(m.older)}
          </ActionButton>
        )}
      </span>
    </nav>
  );
}
