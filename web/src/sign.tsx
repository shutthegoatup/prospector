// The page someone outside an organisation signs on, from a link mailed to them. It
// has none of the members' app: it reads and writes only through the signing API
// (api/signer), by the cookie its link opens.
import createFetchClient from "openapi-fetch";
import { StrictMode, useCallback, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { type Signed, SignForm } from "@/components/app/signing";
import type { components, paths } from "./signer-api";

type Signing = components["schemas"]["Signing"];

const api = createFetchClient<paths>({ baseUrl: "", headers: { "Content-Type": "application/json" } });

function problem(e: unknown) {
  return (e as { detail?: string } | undefined)?.detail ?? "That didn't work. Reload the page and try again.";
}

function Title({ children }: { children: React.ReactNode }) {
  return (
    <h1 className="font-heading text-3xl font-bold tracking-tight">
      {children}
      <span className="text-coral">.</span>
    </h1>
  );
}

const button = "h-10 rounded-lg border-2 border-ink bg-primary px-4 font-semibold shadow-[2px_2px_0_var(--ink)] disabled:opacity-50";
const quiet = "h-9 rounded-lg border-2 border-ink bg-card px-3 text-sm font-semibold disabled:opacity-50";

function SignPage() {
  const [s, setS] = useState<Signing | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [codeSent, setCodeSent] = useState("");
  const [code, setCode] = useState("");

  useEffect(() => {
    const token = location.pathname.replace(/^\/s\/?/, "");
    (async () => {
      const r = token ? await api.POST("/s/api/v1/open", { body: { link: token } }) : await api.GET("/s/api/v1/signing");
      // The link opened this browser's session; the address keeps nothing of it.
      if (token) history.replaceState(null, "", "/s/");
      if (r.error) setError(problem(r.error));
      else setS(r.data);
    })();
  }, []);

  const load = useCallback(async () => {
    const r = await api.GET("/s/api/v1/document", { parseAs: "arrayBuffer" });
    if (r.error || !r.data) throw new Error("The document couldn't be fetched.");
    return r.data;
  }, []);

  const act = async (f: () => Promise<{ data?: Signing; error?: unknown }>) => {
    setBusy(true);
    setError("");
    const r = await f();
    setBusy(false);
    if (r.error) setError(problem(r.error));
    else if (r.data) setS(r.data);
  };

  if (!s) {
    return error ? (
      <>
        <Title>This link doesn't open anything</Title>
        <p className="text-sm text-muted-foreground">{error} If you still need to sign, ask whoever sent it to send you the link again.</p>
      </>
    ) : (
      <p className="font-mono text-xs text-muted-foreground">Opening…</p>
    );
  }

  if (!s.proven) {
    return (
      <>
        <Title>
          {s.org} asked you to sign {s.agreement}
        </Title>
        <section className="flex flex-col gap-3 panel p-4">
          <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">First, confirm it's you</span>
          {!codeSent ? (
            <>
              <p className="text-sm text-muted-foreground">
                We'll email a 6-digit code to <b className="text-foreground">{s.sent_to}</b>, the address this was sent to. The document opens once you enter it.
              </p>
              <button
                type="button"
                disabled={busy}
                className={button + " self-start"}
                onClick={async () => {
                  setBusy(true);
                  setError("");
                  const r = await api.POST("/s/api/v1/code");
                  setBusy(false);
                  if (r.error) setError(problem(r.error));
                  else setCodeSent(r.data.sent_to);
                }}
              >
                Email me a code
              </button>
            </>
          ) : (
            <form
              className="flex flex-col gap-3"
              onSubmit={(e) => {
                e.preventDefault();
                act(() => api.POST("/s/api/v1/code/check", { body: { code } }));
              }}
            >
              <p className="text-sm text-muted-foreground">
                We've emailed a code to <b className="text-foreground">{codeSent}</b>. It lasts ten minutes.
              </p>
              <label className="flex flex-col gap-1">
                <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">Code</span>
                <input
                  value={code}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  onChange={(e) => setCode(e.currentTarget.value.replace(/\D/g, ""))}
                  className="w-40 rounded-lg border-2 border-ink bg-card px-3 font-mono text-2xl" style={{ height: 48, letterSpacing: "0.3em" }}
                />
              </label>
              <div className="flex flex-wrap gap-3">
                <button type="submit" disabled={busy || code.length !== 6} className={button}>
                  Continue
                </button>
                <button type="button" disabled={busy} className={quiet} onClick={() => setCodeSent("")}>
                  Send a new code
                </button>
              </div>
            </form>
          )}
          {error && (
            <p className="text-sm text-[var(--stop)]" role="alert">
              {error}
            </p>
          )}
          <p className="font-mono text-xs text-muted-foreground">Only the address this was sent to can sign. Not you? Ignore the email.</p>
        </section>
        <p className="border-t border-dashed border-border pt-3 font-mono text-xs text-muted-foreground">
          You don't need an account. {s.org} uses Prospector to send this. Each step you take is recorded for the certificate of signing: the time, your IP
          address and your browser.
        </p>
      </>
    );
  }

  const others = (
    <section className="overflow-hidden panel">
      {s.signers.map((x, i) => (
        <div key={i} className="flex justify-between gap-3 border-b border-border px-4 py-3 text-sm last:border-0">
          <b>
            {i + 1} · {x.name}
            {x.you ? " (you)" : ""}
          </b>
          <span className={"font-mono text-xs " + (x.signed ? "text-[var(--ok)]" : "text-muted-foreground")}>{x.signed ? "signed" : "not yet"}</span>
        </div>
      ))}
    </section>
  );

  switch (s.state) {
    case "sign":
      return (
        <>
          <header className="flex flex-wrap items-baseline justify-between gap-3">
            <Title>Sign the agreement</Title>
            <span className="font-mono text-xs text-muted-foreground">
              {s.agreement} · from {s.org}
            </span>
          </header>
          {s.message && <blockquote className="whitespace-pre-wrap rounded-lg bg-muted px-3 py-2 text-sm">{s.message}</blockquote>}
          <SignForm
            places={s.fields ?? []}
            load={load}
            sentName={s.name ?? ""}
            consent={s.consent}
            busy={busy}
            error={error}
            onSign={(x: Signed) => act(() => api.POST("/s/api/v1/sign", { body: { typed_name: x.typed, consent: true, values: x.values } }))}
            onDecline={(reason) => act(() => api.POST("/s/api/v1/decline", { body: { reason } }))}
          />
        </>
      );
    case "waiting":
      return (
        <>
          <Title>It isn't your turn yet</Title>
          <p className="text-sm text-muted-foreground">They sign in order. You'll get an email when it's yours.</p>
          {others}
        </>
      );
    case "signed":
    case "sealing":
      return (
        <>
          <Title>You've signed</Title>
          <p className="text-sm text-muted-foreground">
            {s.state === "sealing"
              ? "Everyone has signed: it's being sealed now. You'll get an email with the signed copy."
              : "When everyone has signed, you'll get an email with the sealed copy and its certificate."}
          </p>
          {others}
        </>
      );
    case "completed":
      return (
        <>
          <Title>Signed by everyone</Title>
          <p className="text-sm text-muted-foreground">
            {s.agreement} has been signed and sealed. The certificate of signing is its last page.
          </p>
          <a href="/s/api/v1/sealed" className={button + " inline-flex items-center self-start no-underline"}>
            Download the signed copy
          </a>
          {others}
        </>
      );
    case "declined":
      return (
        <>
          <Title>This agreement was declined</Title>
          <p className="text-sm text-muted-foreground">It can't be signed now. {s.org} has been told.</p>
        </>
      );
    case "expired":
      return (
        <>
          <Title>The time to sign this has run out</Title>
          <p className="text-sm text-muted-foreground">Ask {s.org} to send it again.</p>
        </>
      );
    case "copy":
      return (
        <>
          <Title>You'll get a copy</Title>
          <p className="text-sm text-muted-foreground">Once everyone has signed, the sealed copy comes to you by email.</p>
          {others}
        </>
      );
  }
  return (
    <>
      <Title>This agreement was withdrawn</Title>
      <p className="text-sm text-muted-foreground">{s.org} withdrew it. There is nothing for you to sign.</p>
    </>
  );
}

const mount = document.getElementById("sign");
if (mount) {
  createRoot(mount).render(
    <StrictMode>
      <main className="mx-auto flex max-w-3xl flex-col gap-5 px-4 pb-16 pt-10">
        <SignPage />
      </main>
    </StrictMode>,
  );
}
