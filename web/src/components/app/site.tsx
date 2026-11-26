import type { ComponentProps, ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

// The public site's look, on the app's shadcn components: ink edges, offset shadows in
// the section's colour, chunky buttons that lift on hover.

export type Tone = "gold" | "coral" | "green" | "violet";

export const toneFill: Record<Tone, string> = {
  gold: "bg-primary",
  coral: "bg-chart-3",
  green: "bg-chart-2",
  violet: "bg-chart-4",
};

const toneShadow: Record<Tone | "ink", string> = {
  gold: "shadow-[8px_8px_0_var(--chart-1)]!",
  coral: "shadow-[8px_8px_0_var(--chart-3)]!",
  green: "shadow-[8px_8px_0_var(--chart-2)]!",
  violet: "shadow-[8px_8px_0_var(--chart-4)]!",
  ink: "shadow-[8px_8px_0_var(--ink)]!",
};

const toneShadowBig: Record<Tone | "ink", string> = {
  gold: "md:shadow-[14px_14px_0_var(--chart-1)]!",
  coral: "md:shadow-[14px_14px_0_var(--chart-3)]!",
  green: "md:shadow-[14px_14px_0_var(--chart-2)]!",
  violet: "md:shadow-[14px_14px_0_var(--chart-4)]!",
  ink: "md:shadow-[14px_14px_0_var(--ink)]!",
};

export function Wrap({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("mx-auto w-full max-w-[1180px] px-5", className)} {...props} />;
}

export function Eyebrow({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("font-mono text-xs font-medium uppercase tracking-[0.12em] text-muted-foreground", className)} {...props} />;
}

export function SectionHead({ label, title, children, className }: { label: ReactNode; title: string; children?: ReactNode; className?: string }) {
  return (
    <div className={cn("mb-14 max-w-[720px]", className)}>
      {typeof label === "string" ? <Eyebrow>{label}</Eyebrow> : label}
      <h2 className="mt-3.5 mb-4 font-heading text-[clamp(30px,3.8vw,46px)] font-bold leading-[1.08] tracking-[-0.02em] text-balance">{title}</h2>
      {children && <p className="text-lg opacity-80">{children}</p>}
    </div>
  );
}

// Tag is a Badge in one of the section colours, for step numbers and lists' headings.
export function Tag({ tone = "gold", className, ...props }: ComponentProps<typeof Badge> & { tone?: Tone }) {
  return (
    <Badge
      className={cn("rounded-md px-2.5 py-1 font-mono text-xs font-medium uppercase tracking-[0.1em] text-ink", toneFill[tone], className)}
      {...props}
    />
  );
}

type Look = "ink" | "outline" | "gold" | "light";

const looks: Record<Look, string> = {
  ink: "border-ink bg-ink text-background hover:bg-ink hover:shadow-[4px_4px_0_var(--coral)]!",
  outline: "border-ink bg-transparent text-ink hover:bg-transparent hover:shadow-[4px_4px_0_var(--ink)]!",
  gold: "border-primary bg-primary text-ink hover:bg-primary hover:shadow-[4px_4px_0_var(--coral)]!",
  light: "border-background bg-transparent text-background hover:bg-transparent hover:shadow-[4px_4px_0_var(--background)]!",
};

// SiteButton is a link drawn as the site's button.
export function SiteButton({ look = "ink", big, small, className, ...props }: ComponentProps<"a"> & { look?: Look; big?: boolean; small?: boolean }) {
  return (
    <Button
      asChild
      className={cn(
        "rounded-[10px] border-2 font-semibold transition-[transform,box-shadow] hover:-translate-x-0.5 hover:-translate-y-0.5",
        big ? "h-12 px-6 text-[15px]" : small ? "h-9 px-4 text-sm" : "h-11 px-5 text-[15px]",
        looks[look],
        className,
      )}
    >
      <a {...props} />
    </Button>
  );
}

// Panel is a Card with the site's offset shadow.
export function Panel({ tone, big, className, ...props }: ComponentProps<typeof Card> & { tone?: Tone | "ink"; big?: boolean }) {
  return <Card className={cn("gap-0 py-0", tone && toneShadow[tone], tone && big && toneShadowBig[tone], className)} {...props} />;
}

export function Shot({ file, width, height, alt, tone }: { file: string; width: number; height: number; alt: string; tone: Tone }) {
  return (
    <Panel tone={tone} big className="overflow-hidden bg-sidebar!">
      <img className="block h-auto w-full" src={`/assets/img/${file}`} width={width} height={height} loading="lazy" alt={alt} />
    </Panel>
  );
}

// Terminal is a dark Card of shell lines: $ prompts in gold, quoted values in green.
export function Terminal({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    <Panel className={cn("overflow-hidden border-sidebar-border! bg-[#0f0c07]! text-[#e9e2d4]", className)}>
      <div className="border-b border-sidebar-border px-[18px] py-3">
        <Eyebrow className="text-[#b9b0a0]">{title}</Eyebrow>
      </div>
      <div className="overflow-x-auto px-[18px] py-5 font-mono text-sm leading-7 whitespace-nowrap [&_b]:font-normal [&_b]:text-primary [&_b]:select-none [&_i]:not-italic [&_i]:text-chart-2 [&>span]:block">
        {children}
      </div>
    </Panel>
  );
}

export function Brand({ light }: { light?: boolean }) {
  return (
    <a className={cn("flex items-center gap-2.5 font-heading text-[21px] font-bold tracking-[-0.01em]", light && "text-background")} href="/">
      <img src="/assets/img/mark.svg" width={30} height={30} alt="" />
      <span>Prospector.</span>
    </a>
  );
}

export function Footer({ blurb, children }: { blurb: ReactNode; children: ReactNode }) {
  return (
    <footer className="bg-sidebar text-[#b9b0a0]">
      <Wrap className="flex flex-wrap items-center gap-x-7 gap-y-4 py-7 text-sm">
        <Brand light />
        <span>{blurb}</span>
        <nav aria-label="Footer" className="flex flex-wrap gap-6 md:ml-auto [&_a:hover]:text-background">
          {children}
        </nav>
      </Wrap>
    </footer>
  );
}
