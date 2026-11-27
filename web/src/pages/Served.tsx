import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { defineMessage, defineMessages, useIntl } from "react-intl";
import { $api } from "@/client";
import { ActionButton, stop } from "@/components/app";
import { Problem, SignedOutPage, SignedOutTitle } from "@/components/app/signed-out";
import { onDay } from "@/lib/format";
import { failure } from "@/lib/sign-in";
import { stepMeta, type Served } from "@/served";

const m = defineMessages({
  backHome: { defaultMessage: "Back to Prospector", id: "+m6MKx", description: "Button on an error page, back to the app" },
  firstTitle: { defaultMessage: "Your first organisation", id: "Q5ZL9W", description: "Title of the page to start a first organisation" },
  firstWhat: {
    defaultMessage: "The company you're prospecting for. You can add more later and switch between them.",
    id: "6efxNd",
    description: "What an organisation is, on the page to start a first one",
  },
  orgName: { defaultMessage: "Organisation name", id: "T6kmYe", description: "Placeholder and label of the field naming a new organisation" },
  create: { defaultMessage: "Create", id: "83KaBa", description: "Button to create the first organisation" },
  noConnection: {
    defaultMessage: "That didn't go through. Check your connection and try again.",
    id: "d68oa4",
    description: "When creating an organisation failed with nothing from the server to say why",
  },
  unsubscribe: { defaultMessage: "Unsubscribe", id: "HTJdgK", description: "Title of the page an unsubscribe link opens, and its button" },
  unsubscribed: { defaultMessage: "You won't get these again", id: "HkI9B7", description: "Heading after unsubscribing" },
  unsubscribedWhat: {
    defaultMessage:
      "Marketing mail from this sender won't go to the address this link was sent to. Mail about something you already have with them, such as a change to your contract, can still reach you.",
    id: "b7VNor",
    description: "After unsubscribing: what stops, and what can still come",
  },
  unsubscribeWhat: {
    defaultMessage: "Stop marketing mail from this sender to the address this link was sent to.",
    id: "1OUt4r",
    description: "What the unsubscribe button does",
  },
  notOurs: {
    defaultMessage: "This link isn't one we sent. Use the link in the mail itself.",
    id: "OLWuEw",
    description: "On an unsubscribe page whose link is malformed",
  },
  tab: { defaultMessage: "Your onboarding", id: "OR1PS+", description: "Browser tab title of a customer's onboarding page, when the organisation isn't known" },
  openWhat: {
    defaultMessage: "It shows the steps that are yours while they set you up. Opening it here keeps it open in this browser for 30 days. The link works once.",
    id: "lhN4WM",
    description: "What a customer's onboarding page is, before they open it",
  },
  open: { defaultMessage: "Open my steps", id: "xkqhRH", description: "Button to open a customer's onboarding page from their link" },
  againTitle: {
    defaultMessage: "This link has been used or has run out",
    id: "tNR2Cj",
    description: "Heading when a customer's link no longer opens their page",
  },
  againWhat: {
    defaultMessage: "A link opens the page once, in one browser. We can send a fresh one to the address this one was sent to.",
    id: "CYJzap",
    description: "Why a customer's link stopped working, and what can be done",
  },
  sendAgain: { defaultMessage: "Send me a new link", id: "OP+zjF", description: "Button to mail a customer a fresh link" },
  sentTitle: { defaultMessage: "Check your email", id: "LTWfbQ", description: "Heading after asking for a fresh link" },
  sentWhat: {
    defaultMessage:
      "If that link was yours, a new one is on its way to the address it was sent to. It can take a minute, and only one is sent every few minutes.",
    id: "T5oLiE",
    description: "After asking for a fresh link; it says the same whether or not the link was real",
  },
  stepsTitle: { defaultMessage: "Getting you set up", id: "gsLlgl", description: "Heading of a customer's onboarding page" },
  nothing: {
    defaultMessage: "There is nothing for you to do at the moment.",
    id: "Ls0JRD",
    description: "On a customer's onboarding page with no steps of theirs",
  },
  markDone: { defaultMessage: "Mark done", id: "rW3MyB", description: "Button for a customer to mark one of their steps done" },
  notDone: { defaultMessage: "Not done yet", id: "YnwcFA", description: "Button for a customer to take back marking a step done" },
  close: { defaultMessage: "Close it here", id: "bTlt3J", description: "Button to close a customer's onboarding page in this browser" },
  noneTitle: { defaultMessage: "Nothing to show here", id: "jtASEY", description: "Heading of /c opened without a link" },
  noneWhat: {
    defaultMessage:
      "This page opens from a link in an email. If yours has stopped working, open it again and ask for a new one, or ask whoever is setting you up.",
    id: "0HxoFB",
    description: "On /c opened without a link: how to get to a customer's page",
  },
});

const v = {
  orgTab: defineMessage<{ org: string }>({
    defaultMessage: "{org} · your onboarding",
    id: "hMigST",
    description: "Browser tab title of a customer's onboarding page. {org} is who is setting them up",
  }),
  openTitle: defineMessage<{ org: string | undefined }>({
    defaultMessage: "{org} set up a page for you",
    id: "B/Gqh6",
    description: "Heading when a customer opens the link to their onboarding page. {org} is who is setting them up",
  }),
  progress: defineMessage<{ done: number; total: number }>({
    defaultMessage: "{done} of {total, plural, one {# of your steps is} other {# of your steps are}} done. Mark each one when you have done it.",
    id: "Xm3zph",
    description: "How many of a customer's own onboarding steps are done",
  }),
  stepMeta: defineMessage<{ said: string; on: string }>({
    defaultMessage: "{said, select, done {done {on}} late {was due {on}} other {due {on}}}",
    id: "slbFYd",
    description: "Beside a customer's step: when it was done, or when it was or is due. {on} is a date",
  }),
  openFor: defineMessage<{ email: string | undefined }>({
    defaultMessage: "This page is open for {email} in this browser.",
    id: "/SrQDl",
    description: "At the foot of a customer's onboarding page",
  }),
};

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

const dot = <span className="text-coral">{stop}</span>;

// A problem's title and message are the server's, already in the page's language.
function ProblemPage({ code, title, message, back }: Extract<Served, { page: "problem" }>) {
  const say = useIntl().formatMessage;
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
        <a href={back}>{say(m.backHome)}</a>
      </ActionButton>
    </SignedOutPage>
  );
}

function StartPage() {
  const say = useIntl().formatMessage;
  const [name, setName] = useState("");
  const create = $api.useMutation("post", "/api/v1/orgs", {
    onSuccess: (o) => window.location.assign(`/o/${encodeURIComponent(o.slug)}/settings`),
  });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    create.mutate({ body: { name } });
  };
  return (
    <SignedOutPage title={say(m.firstTitle)}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>{say(m.firstTitle)}</SignedOutTitle>
        <p className="text-sm text-muted-foreground">{say(m.firstWhat)}</p>
        <input
          name="name"
          required
          placeholder={say(m.orgName)}
          aria-label={say(m.orgName)}
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="h-10 rounded-lg border border-input bg-muted px-3"
        />
        <ActionButton type="submit" disabled={create.isPending || create.isSuccess}>
          {say(m.create)}
        </ActionButton>
        <Problem>{failure(create.error, say(m.noConnection))}</Problem>
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
  const say = useIntl().formatMessage;
  if (done) {
    return (
      <OutsidePage title={say(m.unsubscribe)}>
        <Title>{say(m.unsubscribed)}</Title>
        <Said>{say(m.unsubscribedWhat)}</Said>
      </OutsidePage>
    );
  }
  return (
    <OutsidePage title={say(m.unsubscribe)}>
      <Title mark="?">{say(m.unsubscribe)}</Title>
      <Said>{say(m.unsubscribeWhat)}</Said>
      {token ? <Post to={`/u/${encodeURIComponent(token)}`}>{say(m.unsubscribe)}</Post> : <Said>{say(m.notOurs)}</Said>}
    </OutsidePage>
  );
}

function CustomerPage({ state, token, org, email, day, steps }: Extract<Served, { page: "customer" }>) {
  const say = useIntl().formatMessage;
  const title = org ? say(v.orgTab, { org }) : say(m.tab);
  switch (state) {
    case "open":
      return (
        <OutsidePage title={title}>
          <Title>{say(v.openTitle, { org })}</Title>
          <Said>{say(m.openWhat)}</Said>
          {token && <Post to={linkPath(token)}>{say(m.open)}</Post>}
        </OutsidePage>
      );
    case "again":
      return (
        <OutsidePage title={title}>
          <Title>{say(m.againTitle)}</Title>
          <Said>{say(m.againWhat)}</Said>
          {token && <Post to={`${linkPath(token)}/again`}>{say(m.sendAgain)}</Post>}
        </OutsidePage>
      );
    case "sent":
      return (
        <OutsidePage title={title}>
          <Title>{say(m.sentTitle)}</Title>
          <Said>{say(m.sentWhat)}</Said>
        </OutsidePage>
      );
    case "steps": {
      const done = steps.filter((st) => st.done_on).length;
      return (
        <OutsidePage title={title}>
          <header className="flex flex-wrap items-baseline justify-between gap-3">
            <Title>{say(m.stepsTitle)}</Title>
            <span className="font-mono text-xs text-muted-foreground">{org}</span>
          </header>
          {steps.length === 0 ? (
            <Said>{say(m.nothing)}</Said>
          ) : (
            <>
              <Said>{say(v.progress, { done, total: steps.length })}</Said>
              <div className="overflow-hidden panel">
                {steps.map((st) => {
                  const meta = stepMeta(st, day);
                  return (
                    <div key={st.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3 text-sm last:border-0">
                      {st.done_on ? <span className="text-muted-foreground">{st.name}</span> : <b>{st.name}</b>}
                      <span className="flex flex-wrap items-center gap-3">
                        <span className="font-mono text-xs text-muted-foreground">{meta && say(v.stepMeta, { said: meta.said, on: onDay(meta.on, day) })}</span>
                        {!st.done_on ? (
                          <Post to={`/c/steps/${st.id}/done`} small>
                            {say(m.markDone)}
                          </Post>
                        ) : (
                          st.mine && (
                            <Post to={`/c/steps/${st.id}/undone`} small quiet>
                              {say(m.notDone)}
                            </Post>
                          )
                        )}
                      </span>
                    </div>
                  );
                })}
              </div>
            </>
          )}
          <form method="post" action="/c/leave" className="flex flex-wrap items-center gap-3">
            <span className="font-mono text-xs text-muted-foreground">{say(v.openFor, { email })}</span>
            <ActionButton type="submit" size="sm" quiet>
              {say(m.close)}
            </ActionButton>
          </form>
        </OutsidePage>
      );
    }
    default:
      return (
        <OutsidePage title={title}>
          <Title>{say(m.noneTitle)}</Title>
          <Said>{say(m.noneWhat)}</Said>
        </OutsidePage>
      );
  }
}
