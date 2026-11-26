import { Fragment, type ReactNode } from "react";
import { Brand, Eyebrow, Footer, Panel, SiteButton, Wrap } from "@/components/app/site";
import { Badge } from "@/components/ui/badge";
import { CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

// The shape of apidoc.json, which the server's test writes from api/openapi.yaml.
export type DocSpan = { text: string; code?: boolean };
export type DocType = { name: string; link?: string };
export type DocField = { name: string; type: DocType; about: DocSpan[]; required?: boolean; write?: boolean };
export type DocResponse = { code: string; bad?: boolean; about: string; type?: DocType };
export type DocOp = {
  anchor: string;
  method: string;
  tone: string;
  path: string;
  summary: string;
  about: DocSpan[];
  scope?: string;
  owner_only?: boolean;
  params: DocField[];
  body?: { of?: DocType; fields: DocField[] };
  responses: DocResponse[];
  curl: string;
};
export type DocSchema = { anchor: string; name: string; about: DocSpan[]; enum: string[]; fields: DocField[] };
export type Doc = {
  title: string;
  version: string;
  intro: DocSpan[][];
  scopes: { name: string; what: string; owner?: boolean }[];
  groups: { name: string; about: DocSpan[]; ops: DocOp[] }[];
  schemas: DocSchema[];
  problems: { anchor: string; type: string; about: DocSpan[] }[];
};

const repoURL = "https://github.com/shutthegoatup/prospector";

const methodFill: Record<string, string> = {
  "is-green": "bg-chart-2",
  "is-gold": "bg-primary",
  "is-coral": "bg-chart-3",
  "is-violet": "bg-chart-4",
};

// Inline code and links in running text, as the reference writes them.
const prose = "[&_code]:rounded [&_code]:bg-ink/[0.06] [&_code]:px-[5px] [&_code]:py-px [&_code]:font-mono [&_code]:text-[0.88em] [&_code]:font-medium [&_a]:underline [&_a]:decoration-primary [&_a]:underline-offset-[3px]";

function Spans({ of }: { of: DocSpan[] }) {
  return (
    <>
      {of.map((sp, i) => (
        <Fragment key={i}>{sp.code ? <code>{sp.text}</code> : sp.text}</Fragment>
      ))}
    </>
  );
}

function Method({ op, big }: { op: DocOp; big?: boolean }) {
  return (
    <Badge
      className={cn(
        "rounded-[5px] font-mono font-semibold tracking-[0.06em] text-ink",
        big ? "min-w-16 px-2.5 py-1 text-[13px]" : "min-w-[46px] px-1.5 py-0.5 text-[11px]",
        methodFill[op.tone] ?? "bg-secondary",
      )}
    >
      {op.method}
    </Badge>
  );
}

function Flag({ required, children }: { required?: boolean; children: ReactNode }) {
  return (
    <Badge
      variant="outline"
      className={cn("rounded px-1.5 py-px font-mono text-[11px] uppercase tracking-[0.06em] text-muted-foreground", required && "border-chart-3 text-[#b8401f]")}
    >
      {children}
    </Badge>
  );
}

function TypeName({ t }: { t: DocType }) {
  const style = "font-mono text-[13px] text-[#6a55e8] no-underline!";
  return t.link ? (
    <a className={style} href={t.link}>
      {t.name}
    </a>
  ) : (
    <span className={style}>{t.name}</span>
  );
}

function Fields({ of }: { of: DocField[] }) {
  return (
    <dl className="divide-y divide-border">
      {of.map((f) => (
        <div className="py-2" key={f.name}>
          <dt className="flex flex-wrap items-center gap-2">
            <code className="bg-transparent! p-0! font-semibold">{f.name}</code>
            <TypeName t={f.type} />
            {f.required && <Flag required>required</Flag>}
            {f.write && <Flag>write-only</Flag>}
          </dt>
          {f.about.length > 0 && (
            <dd className="mt-1 text-[15px] text-muted-foreground">
              <Spans of={f.about} />
            </dd>
          )}
        </div>
      ))}
    </dl>
  );
}

function Code({ dark, children }: { dark?: boolean; children: string }) {
  return (
    <pre
      className={cn(
        "overflow-x-auto rounded-[10px] border px-4 py-3.5 font-mono text-[13px] leading-[1.6] [&_code]:bg-transparent! [&_code]:p-0! [&_code]:text-[length:inherit]!",
        dark ? "sticky top-5 border-sidebar bg-sidebar break-all whitespace-pre-wrap text-[#f4ecdc]" : "my-2.5 mb-3.5 border-border bg-card whitespace-pre",
      )}
    >
      <code>{children}</code>
    </pre>
  );
}

function Heading({ children, className }: { children: ReactNode; className?: string }) {
  return <h2 className={cn("mb-3.5 font-heading text-[30px] leading-[1.15] font-bold tracking-[-0.02em]", className)}>{children}</h2>;
}

function Section({ id, children }: { id?: string; children: ReactNode }) {
  return (
    <section id={id} className="mb-9 border-b border-dashed border-border pb-7 last:border-b-0 [&>p]:mb-3 [&>p]:max-w-[70ch]">
      {children}
    </section>
  );
}

function SubHead({ children }: { children: ReactNode }) {
  return <h4 className="mt-4 mb-1.5 font-mono text-xs font-medium uppercase tracking-[0.12em] text-muted-foreground first:mt-0">{children}</h4>;
}

const problemExample = `HTTP/1.1 403 Forbidden
Content-Type: application/problem+json

{"type":"about:blank","title":"Forbidden","status":403,"detail":"this key lacks the agents:configure scope"}`;

// ApiReference is the page at /api: the API's contract, as people read it. It is drawn
// when the site is built and runs no script.
export function ApiReference({ doc }: { doc: Doc }) {
  return (
    <div className={prose}>
      <header className="border-b border-border bg-[linear-gradient(180deg,#fff3cf_0%,var(--background)_100%)]">
        <Wrap>
          <nav className="flex items-center justify-between gap-7 py-5" aria-label="Site">
            <Brand />
            <div className="ml-auto hidden gap-6 text-[15px] font-medium no-underline sm:flex [&_a]:no-underline! [&_a]:opacity-75 [&_a:hover]:opacity-100">
              <a href="/">Product</a>
              <a href="#auth">Authentication</a>
              <a href="#schemas">Schemas</a>
            </div>
            <SiteButton look="outline" small href="/api/openapi.json" className="no-underline!">
              openapi.json
            </SiteButton>
          </nav>
          <div className="max-w-[760px] pt-10 pb-12">
            <Eyebrow>{`${doc.title} API · v${doc.version} · OpenAPI 3`}</Eyebrow>
            <h1 className="mt-3.5 mb-[18px] font-heading text-[clamp(40px,6vw,64px)] leading-[1.02] font-bold tracking-[-0.03em]">The contract.</h1>
            {doc.intro.map((p, i) => (
              <p className="mt-3 text-lg text-muted-foreground" key={i}>
                <Spans of={p} />
              </p>
            ))}
          </div>
        </Wrap>
      </header>
      <Wrap className="grid gap-12 pt-10 pb-20 lg:grid-cols-[230px_minmax(0,1fr)]">
        <aside
          className="sticky top-5 hidden max-h-[calc(100vh-40px)] flex-col gap-0.5 self-start overflow-auto pr-2 lg:flex [&_a]:flex [&_a]:items-center [&_a]:gap-2 [&_a]:rounded-md [&_a]:px-2 [&_a]:py-[5px] [&_a]:text-sm [&_a]:text-muted-foreground [&_a]:no-underline! [&_a:hover]:bg-card [&_a:hover]:text-foreground"
          aria-label="Contents"
        >
          <Eyebrow className="mb-1.5">Start</Eyebrow>
          <a href="#auth">Authentication</a>
          <a href="#errors">Errors</a>
          {doc.groups.map((g) => (
            <Fragment key={g.name}>
              <Eyebrow className="mt-[18px] mb-1.5">{g.name}</Eyebrow>
              {g.ops.map((o) => (
                <a key={o.anchor} href={`#${o.anchor}`}>
                  <Method op={o} />
                  {o.summary}
                </a>
              ))}
            </Fragment>
          ))}
          <Eyebrow className="mt-[18px] mb-1.5">Schemas</Eyebrow>
          {doc.schemas.map((s) => (
            <a key={s.anchor} href={`#${s.anchor}`}>
              {s.name}
            </a>
          ))}
        </aside>
        <main className="min-w-0">
          <Section id="auth">
            <Heading>Authentication</Heading>
            <p>
              Send an API key made on your organisation's Agents page as a bearer token. Every path starts with your organisation's slug, and a key only opens its
              own organisation.
            </p>
            <Code>Authorization: Bearer pk_…</Code>
            <p>Prospector's own pages use the same API with the signed-in session. A member can read and run; changing settings needs an owner.</p>
            <div className="overflow-x-auto">
              <table className="mt-3 w-full border-collapse text-[15px]">
                <thead>
                  <tr className="border-b border-border text-left font-mono text-xs uppercase tracking-[0.12em] text-muted-foreground">
                    <th className="py-2 pr-2.5 font-medium">Scope</th>
                    <th className="py-2 pr-2.5 font-medium">Lets a key</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {doc.scopes.map((s) => (
                    <tr key={s.name} className="align-top">
                      <td className="py-2.5 pr-2.5 md:whitespace-nowrap">
                        <code>{s.name}</code> {s.owner && <Flag>owner-level</Flag>}
                      </td>
                      <td className="py-2.5 pr-2.5">{s.what}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Section>
          <Section id="errors">
            <Heading>Errors</Heading>
            <p>
              A failure answers with its status and an <a href="https://www.rfc-editor.org/rfc/rfc9457">RFC 9457</a> problem, whose <code>detail</code> says what to
              change.
            </p>
            <Code>{problemExample}</Code>
            <p>
              Each operation lists the statuses it can answer with. A problem a client can act on without reading <code>detail</code> has a <code>type</code> that
              links here:
            </p>
            <dl className="divide-y divide-border">
              {doc.problems.map((p) => (
                <div className="scroll-mt-5 py-2" id={p.anchor} key={p.type}>
                  <dt>
                    <code>{p.type}</code>
                  </dt>
                  <dd className="mt-1 text-[15px] text-muted-foreground">
                    <Spans of={p.about} />
                  </dd>
                </div>
              ))}
            </dl>
          </Section>
          {doc.groups.map((g) => (
            <Section key={g.name}>
              <Heading className="capitalize">{g.name}</Heading>
              {g.about.length > 0 && (
                <p className="text-muted-foreground">
                  <Spans of={g.about} />
                </p>
              )}
              {g.ops.map((o) => (
                <article id={o.anchor} key={o.anchor} className="mt-[18px] scroll-mt-5">
                <Panel className="border! border-border! px-4 py-[18px] shadow-[0_1px_0_rgba(28,24,16,0.04),0_10px_30px_-18px_rgba(28,24,16,0.25)]! md:px-6 md:py-[22px]">
                  <header className="flex flex-wrap items-center gap-3">
                    <Method op={o} big />
                    <code className="bg-transparent! p-0! text-[15px] break-all">{o.path}</code>
                  </header>
                  <CardTitle className="mt-3.5 mb-1.5 font-heading text-xl leading-[1.3]">{o.summary}</CardTitle>
                  {o.about.length > 0 && (
                    <p className="max-w-[70ch] text-muted-foreground">
                      <Spans of={o.about} />
                    </p>
                  )}
                  <p className="mt-2.5 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                    {o.scope && (
                      <>
                        Key scope <code>{o.scope}</code>
                      </>
                    )}
                    {o.owner_only && <Flag>owner, when signed in</Flag>}
                  </p>
                  <div className="mt-4 grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,0.9fr)]">
                    <div>
                      {o.params.length > 0 && (
                        <>
                          <SubHead>Path</SubHead>
                          <Fields of={o.params} />
                        </>
                      )}
                      {o.body && (
                        <>
                          <SubHead>
                            Body
                            {o.body.of && (
                              <span className="ml-1.5 normal-case tracking-normal">
                                <TypeName t={o.body.of} />
                              </span>
                            )}
                          </SubHead>
                          <Fields of={o.body.fields} />
                        </>
                      )}
                      <SubHead>Responses</SubHead>
                      <dl className="divide-y divide-border">
                        {o.responses.map((r) => (
                          <div className="py-2" key={r.code}>
                            <dt className="flex flex-wrap items-center gap-2">
                              <Badge
                                className={cn(
                                  "rounded px-[7px] py-px font-mono text-xs font-semibold",
                                  r.bad ? "bg-chart-3/15 text-[#b8401f]" : "bg-chart-2/20 text-[#1d7a52]",
                                )}
                              >
                                {r.code}
                              </Badge>
                              {r.type && <TypeName t={r.type} />}
                            </dt>
                            <dd className="mt-1 text-[15px] text-muted-foreground">{r.about}</dd>
                          </div>
                        ))}
                      </dl>
                    </div>
                    <Code dark>{o.curl}</Code>
                  </div>
                </Panel>
                </article>
              ))}
            </Section>
          ))}
          <Section id="schemas">
            <Heading>Schemas</Heading>
            {doc.schemas.map((s) => (
              <article id={s.anchor} className="scroll-mt-5 border-t border-border py-[18px]" key={s.anchor}>
                <h3 className="mb-1.5 text-[17px]">
                  <code className="bg-transparent! p-0! font-semibold">{s.name}</code>
                </h3>
                {s.about.length > 0 && (
                  <p className="mb-1.5 text-muted-foreground">
                    <Spans of={s.about} />
                  </p>
                )}
                {s.enum.length > 0 && (
                  <p className="flex flex-wrap gap-1.5">
                    {s.enum.map((e) => (
                      <code key={e}>{e}</code>
                    ))}
                  </p>
                )}
                {s.fields.length > 0 && <Fields of={s.fields} />}
              </article>
            ))}
          </Section>
        </main>
      </Wrap>
      <Footer
        blurb={
          <>
            The spec is <a href="/api/openapi.json">openapi.json</a>; generate a client from it in any language.
          </>
        }
      >
        <a href={repoURL}>GitHub</a>
        <a href="/">Product</a>
        <a href="/login">Sign in</a>
      </Footer>
    </div>
  );
}
