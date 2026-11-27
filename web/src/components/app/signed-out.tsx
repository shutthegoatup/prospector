import { useEffect, type ComponentProps, type ReactNode } from "react";
import { defineMessage, useIntl } from "react-intl";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

const backdrop = "front-backdrop min-h-svh font-sans antialiased";

// SignedOutPage is the card the sign-in pages sit in, on the front page's backdrop
// rather than the app's.
const tab = defineMessage<{ page: string }>({
  defaultMessage: "{page} · Prospector",
  id: "ggJSEV",
  description: "The browser tab's title on a page outside any organisation, such as signing in",
});

export function SignedOutPage({ title, children, framed = true }: { title: string; children: ReactNode; framed?: boolean }) {
  const intl = useIntl();
  useEffect(() => {
    document.title = intl.formatMessage(tab, { page: title });
    const was = document.body.className;
    document.body.className = backdrop;
    return () => {
      document.body.className = was;
    };
  }, [intl, title]);
  return (
    <div className="grid min-h-svh place-items-center px-4">
      <main className={cn("flex w-full max-w-sm flex-col gap-4", framed && "panel panel-lift p-7")}>{children}</main>
    </div>
  );
}

export function SignedOutTitle({ children }: { children: ReactNode }) {
  return <h1 className="font-heading text-2xl font-bold">{children}</h1>;
}

export function Field({ label, ...props }: ComponentProps<"input"> & { label: string }) {
  return (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{label}</span>
      <Input className="h-10 rounded-lg bg-muted px-3" {...props} />
    </label>
  );
}

export function Problem({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <p className="text-sm text-[var(--stop)]" role="alert">
      {children}
    </p>
  );
}

export function Notice({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <p className="text-sm text-[var(--ok)]" role="status">
      {children}
    </p>
  );
}
