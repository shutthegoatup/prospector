import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { $api } from "@/client";
import { ActionButton } from "@/components/app";
import { Problem, SignedOutPage, SignedOutTitle } from "@/components/app/signed-out";
import { failure } from "@/lib/sign-in";
import { stepMeta, type Served } from "@/served";

export function ServedPage({ page }: { page: Served }) {
  switch (page.page) {
    case "problem":
      return <ProblemPage {...page} />;
    case "start":
      return <StartPage />;
    case "unsubscribe":
      return <UnsubscribePage {...page} />;
    case "customer":
      return <CustomerPage {...page} />;
  }
}

const dot = <span className="text-coral">.</span>;

function ProblemPage({ code, title, message, back }: Extract<Served, { page: "problem" }>) {
  return (
    <SignedOutPage title={title}>
      <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{code}</span>
      <SignedOutTitle>
        {title}
        {dot}
      </SignedOutTitle>
      <p className="text-sm text-muted-foreground" role="alert">
        {message}
      </p>
      <ActionButton size="lg" asChild>
        <a href={back}>Back to Prospector</a>
      </ActionButton>
    </SignedOutPage>
  );
}

function StartPage() {
  const [name, setName] = useState("");
  const create = $api.useMutation("post", "/api/v1/orgs", {
    onSuccess: (o) => window.location.assign(`/o/${encodeURIComponent(o.slug)}/settings`),
  });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    create.mutate({ body: { name } });
  };
  return (
    <SignedOutPage title="Your first organisation">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>Your first organisation</SignedOutTitle>
        <p className="text-sm text-muted-foreground">The company you're prospecting for. You can add more later and switch between them.</p>
        <input
          name="name"
          required
          placeholder="Organisation name"
          aria-label="Organisation name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-10 rounded-lg border border-input bg-muted px-3"
        />
        <ActionButton type="submit" disabled={create.isPending || create.isSuccess}>
          Create
        </ActionButton>
        <Problem>{failure(create.error)}</Problem>
      </form>
    </SignedOutPage>
  );
}

// OutsidePage is a page for someone outside the organisation: none of the members'
// layout, no sidebar, no links into the app.
function OutsidePage({ title, children }: { title: string; children: ReactNode }) {
  useEffect(() => {
    document.title = title;
  }, [title]);
  return <main className="mx-auto flex max-w-3xl flex-col gap-5 px-4 pb-16 pt-10">{children}</main>;
}

function Title({ children, mark = "." }: { children: ReactNode; mark?: string }) {
  return (
    <h1 className="font-heading text-3xl font-bold tracking-tight">
      {children}
      <span className="text-coral">{mark}</span>
    </h1>
  );
}

function Said({ children }: { children: ReactNode }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}

// Post is a plain form: these pages work by the server's own routes, which answer with
// the page to show next.
function Post({ to, children, quiet, small }: { to: string; children: ReactNode; quiet?: boolean; small?: boolean }) {
  return (
    <form method="post" action={to}>
      <ActionButton type="submit" quiet={quiet} size={small ? "sm" : "default"}>
        {children}
      </ActionButton>
    </form>
  );
}

const linkPath = (token: string) => `/c/${encodeURIComponent(token)}`;

function UnsubscribePage({ token, done }: Extract<Served, { page: "unsubscribe" }>) {
  if (done) {
    return (
      <OutsidePage title="Unsubscribe">
        <Title>You won't get these again</Title>
        <Said>
          Marketing mail from this sender won't go to the address this link was sent to. Mail about something you already have with them, such as a change to
          your contract, can still reach you.
        </Said>
      </OutsidePage>
    );
  }
  return (
    <OutsidePage title="Unsubscribe">
      <Title mark="?">Unsubscribe</Title>
      <Said>Stop marketing mail from this sender to the address this link was sent to.</Said>
      {token ? <Post to={`/u/${encodeURIComponent(token)}`}>Unsubscribe</Post> : <Said>This link isn't one we sent. Use the link in the mail itself.</Said>}
    </OutsidePage>
  );
}

function CustomerPage({ state, token, org, email, day, steps }: Extract<Served, { page: "customer" }>) {
  const title = org ? `${org} · your onboarding` : "Your onboarding";
  switch (state) {
    case "open":
      return (
        <OutsidePage title={title}>
          <Title>{org} set up a page for you</Title>
          <Said>It shows the steps that are yours while they set you up. Opening it here keeps it open in this browser for 30 days. The link works once.</Said>
          {token && <Post to={linkPath(token)}>Open my steps</Post>}
        </OutsidePage>
      );
    case "again":
      return (
        <OutsidePage title={title}>
          <Title>This link has been used or has run out</Title>
          <Said>A link opens the page once, in one browser. We can send a fresh one to the address this one was sent to.</Said>
          {token && <Post to={`${linkPath(token)}/again`}>Send me a new link</Post>}
        </OutsidePage>
      );
    case "sent":
      return (
        <OutsidePage title={title}>
          <Title>Check your email</Title>
          <Said>
            If that link was yours, a new one is on its way to the address it was sent to. It can take a minute, and only one is sent every few minutes.
          </Said>
        </OutsidePage>
      );
    case "steps": {
      const done = steps.filter((st) => st.done_on).length;
      return (
        <OutsidePage title={title}>
          <header className="flex flex-wrap items-baseline justify-between gap-3">
            <Title>Getting you set up</Title>
            <span className="font-mono text-xs text-muted-foreground">{org}</span>
          </header>
          {steps.length === 0 ? (
            <Said>There is nothing for you to do at the moment.</Said>
          ) : (
            <>
              <Said>
                {done} of {steps.length} of your steps are done. Mark each one when you have done it.
              </Said>
              <div className="overflow-hidden panel">
                {steps.map((st) => (
                  <div key={st.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3 text-sm last:border-0">
                    {st.done_on ? <span className="text-muted-foreground">{st.name}</span> : <b>{st.name}</b>}
                    <span className="flex flex-wrap items-center gap-3">
                      <span className="font-mono text-xs text-muted-foreground">{stepMeta(st, day)}</span>
                      {!st.done_on ? (
                        <Post to={`/c/steps/${st.id}/done`} small>
                          Mark done
                        </Post>
                      ) : (
                        st.mine && (
                          <Post to={`/c/steps/${st.id}/undone`} small quiet>
                            Not done yet
                          </Post>
                        )
                      )}
                    </span>
                  </div>
                ))}
              </div>
            </>
          )}
          <form method="post" action="/c/leave" className="flex flex-wrap items-center gap-3">
            <span className="font-mono text-xs text-muted-foreground">This page is open for {email} in this browser.</span>
            <ActionButton type="submit" size="sm" quiet>
              Close it here
            </ActionButton>
          </form>
        </OutsidePage>
      );
    }
    default:
      return (
        <OutsidePage title={title}>
          <Title>Nothing to show here</Title>
          <Said>
            This page opens from a link in an email. If yours has stopped working, open it again and ask for a new one, or ask whoever is setting you up.
          </Said>
        </OutsidePage>
      );
  }
}
