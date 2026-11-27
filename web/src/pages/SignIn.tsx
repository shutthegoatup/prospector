import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useEffect, useRef, type FormEvent } from "react";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import { $api } from "@/client";
import { Field, Notice, Problem, SignedOutPage, SignedOutTitle } from "@/components/app/signed-out";
import { ActionButton, stop } from "@/components/app";
import { safeNext } from "@/lib/next";
import { failure as failed, offered as offeredFrom, withNext } from "@/lib/sign-in";

const m = defineMessages({
  signIn: { defaultMessage: "Sign in", id: "MclTMJ", description: "Title of the sign-in page, and its button" },
  saved: {
    defaultMessage: "Password saved. Sign in with it.",
    id: "TfEvtW",
    description: "On the sign-in page after choosing a new password from a reset link",
  },
  email: { defaultMessage: "Email", id: "tm5UAO", description: "Label of the email address field" },
  password: { defaultMessage: "Password", id: "la6HzQ", description: "Label of the password field" },
  forgot: { defaultMessage: "Forgot your password?", id: "eRNQKX", description: "Link from the sign-in page to reset a password" },
  otherWays: { defaultMessage: "Other ways to sign in", id: "WfNo1q", description: "What a screen reader calls the list of other ways to sign in" },
  or: { defaultMessage: "Or", id: "UAooGb", description: "Small heading above the other ways to sign in" },
  sso: { defaultMessage: "Use your organisation's single sign-on", id: "0o1xO4", description: "Button to sign in through an organisation's own provider" },
  resetTitle: { defaultMessage: "Reset your password", id: "Q937/W", description: "Title of the page to ask for a password reset link" },
  linkSent: {
    defaultMessage: "If that address has an account, a link is on its way. It lasts an hour.",
    id: "Wfckmw",
    description: "After asking for a reset link; it says the same whether or not the address has an account",
  },
  weEmail: { defaultMessage: "We'll email you a link to choose a new one.", id: "eeJYXA", description: "On the page to ask for a password reset link" },
  emailLink: { defaultMessage: "Email me a link", id: "bxJXBH", description: "Button to send a password reset link" },
  back: { defaultMessage: "Back to sign in", id: "gh0Ao+", description: "Link back to the sign-in page" },
  chooseTitle: { defaultMessage: "Choose a password", id: "M6xWaL", description: "Title of the page a reset link opens" },
  newPassword: { defaultMessage: "New password", id: "cVVVDW", description: "Label of the field for a new password" },
  savePassword: { defaultMessage: "Save password", id: "tIzXly", description: "Button to save a new password" },
  ssoTitle: { defaultMessage: "Single sign-on", id: "7QmOpI", description: "Title of the page to sign in through an organisation's own provider" },
  noSSO: {
    defaultMessage: "No organisation here uses single sign-on for that address. Sign in with your password instead.",
    id: "1M9Rh5",
    description: "When no organisation's single sign-on covers the email address given",
  },
  workEmail: { defaultMessage: "Work email", id: "iTA1+m", description: "Label of the email field on the single sign-on page" },
  find: { defaultMessage: "Find my organisation", id: "u+8kmh", description: "Button to look up which organisation's single sign-on covers an address" },
  passwordInstead: {
    defaultMessage: "Sign in with a password instead",
    id: "659pta",
    description: "Link from single sign-on back to signing in with a password",
  },
  ownTitle: {
    defaultMessage: "Choose your own password",
    id: "PuNA1u",
    description: "Title of the page a new member sees first, to replace the password they were given",
  },
  ownWhy: {
    defaultMessage: "The one you signed in with was shown to whoever added you. Choose one only you know, 10 characters or more.",
    id: "do7vTs",
    description: "Why a new member must choose their own password",
  },
  given: { defaultMessage: "Password you were given", id: "UxaOfn", description: "Label of the field for the password a new member was given" },
  continue: { defaultMessage: "Continue", id: "UkToXa", description: "Button to save a new member's own password and go on" },
  signOut: { defaultMessage: "Sign out", id: "QOH77B", description: "Button to sign out" },
  noConnection: {
    defaultMessage: "That didn't go through. Check your connection and try again.",
    id: "1FV3pz",
    description: "When signing in, or the like, failed with nothing from the server to say why",
  },
});

const v = {
  continueWith: defineMessage<{ provider: string }>({
    defaultMessage: "Continue with {provider}",
    id: "yxe9Ac",
    description: "Button to sign in through a provider. {provider} is its name, such as Google",
  }),
  forWhom: defineMessage<{ email: string }>({
    defaultMessage: "For {email}. 10 characters or more.",
    id: "dK/U8c",
    description: "On the page a reset link opens: whose password it is, and how long it must be",
  }),
  asks: defineMessage<{ org: string }>({
    defaultMessage: "{org} asks its people to sign in through its own provider.",
    id: "HriBfZ",
    description: "When an organisation sent someone to its single sign-on. {org} is its name",
  }),
  continueTo: defineMessage<{ org: string }>({
    defaultMessage: "Continue to {org}",
    id: "Qjc+uX",
    description: "Button to an organisation's own sign-in. {org} is its name",
  }),
};

// After signing in the browser goes on as a page, not a route: the server decides where
// a session may go, from choosing a first password to an organisation's own sign-on.
function goOn(next: string | undefined) {
  window.location.assign(safeNext(next));
}

function value(f: FormData, name: string) {
  return String(f.get(name) ?? "");
}

// The name is a name: the same in every language.
const brand = "Prospector.";

const linkClass = "text-sm text-muted-foreground hover:text-foreground";

function useFailure() {
  const intl = useIntl();
  return (error: unknown) => failed(error, intl.formatMessage(m.noConnection));
}

export function SignInPage() {
  const say = useIntl().formatMessage;
  const failure = useFailure();
  const { next, reset } = useSearch({ from: "/login" });
  const ways = $api.useQuery("get", "/api/v1/sign-in");
  const signIn = $api.useMutation("post", "/api/v1/session", { onSuccess: () => goOn(next) });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    signIn.mutate({ body: { email: value(f, "email"), password: value(f, "password") } });
  };
  const w = ways.data;
  return (
    <SignedOutPage title={say(m.signIn)}>
      <h1 className="flex items-center gap-2.5 font-heading text-2xl font-bold">
        <img src="/assets/img/mark.svg" width="30" height="30" alt="" />
        {brand}
      </h1>
      <Problem>{failure(signIn.error)}</Problem>
      {reset && !signIn.error && <Notice>{say(m.saved)}</Notice>}
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label={say(m.email)} name="email" type="email" required autoComplete="username" />
        <Field label={say(m.password)} name="password" type="password" required autoComplete="current-password" />
        <ActionButton type="submit" size="lg" disabled={signIn.isPending || signIn.isSuccess}>
          {say(m.signIn)}
        </ActionButton>
        {w?.forgot && (
          <Link to="/forgot" className={linkClass}>
            {say(m.forgot)}
          </Link>
        )}
      </form>
      {w && (w.social.length > 0 || w.sso) && (
        <nav className="flex flex-col gap-2 border-t border-dashed border-border pt-2" aria-label={say(m.otherWays)}>
          <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{say(m.or)}</span>
          {w.social.map((way) => (
            <ActionButton key={way.key} quiet asChild>
              <a href={withNext(way.start_url, next)}>{say(v.continueWith, { provider: way.label })}</a>
            </ActionButton>
          ))}
          {w.sso && (
            <ActionButton quiet asChild>
              <Link to="/login/sso" search={{ next }}>
                {say(m.sso)}
              </Link>
            </ActionButton>
          )}
        </nav>
      )}
    </SignedOutPage>
  );
}

export function ForgotPage() {
  const say = useIntl().formatMessage;
  const failure = useFailure();
  const ask = $api.useMutation("post", "/api/v1/password-resets");
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    ask.mutate({ body: { email: value(new FormData(e.currentTarget), "email") } });
  };
  return (
    <SignedOutPage title={say(m.resetTitle)}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>{say(m.resetTitle)}</SignedOutTitle>
        {ask.isSuccess ? <Notice>{say(m.linkSent)}</Notice> : <p className="text-sm text-muted-foreground">{say(m.weEmail)}</p>}
        <Problem>{failure(ask.error)}</Problem>
        <Field label={say(m.email)} name="email" type="email" required autoComplete="username" />
        <ActionButton type="submit" size="lg" disabled={ask.isPending}>
          {say(m.emailLink)}
        </ActionButton>
        <Link to="/login" className={linkClass}>
          {say(m.back)}
        </Link>
      </form>
    </SignedOutPage>
  );
}

export function ResetPage() {
  const say = useIntl().formatMessage;
  const failure = useFailure();
  const { token } = useParams({ from: "/reset/$token" });
  // A dead link answers 410, which asking again won't change.
  const link = $api.useQuery("get", "/api/v1/password-resets/{token}", { params: { path: { token } } }, { retry: false });
  const save = $api.useMutation("post", "/api/v1/password-resets/{token}", { onSuccess: () => window.location.assign("/login?reset=1") });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    save.mutate({ params: { path: { token } }, body: { password: value(new FormData(e.currentTarget), "password") } });
  };
  return (
    <SignedOutPage title={say(m.chooseTitle)}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>{say(m.chooseTitle)}</SignedOutTitle>
        {link.data && <p className="text-sm text-muted-foreground">{say(v.forWhom, { email: link.data.email })}</p>}
        <Problem>{failure(link.error ?? save.error)}</Problem>
        <Field label={say(m.newPassword)} name="password" type="password" required minLength={10} autoComplete="new-password" />
        <ActionButton type="submit" size="lg" disabled={save.isPending || save.isSuccess || !link.data}>
          {say(m.savePassword)}
        </ActionButton>
      </form>
    </SignedOutPage>
  );
}

// SingleSignOnPage only ever offers an organisation's sign-in as a link. Going there by
// itself would let a link from another site walk someone through a provider it chose.
export function SingleSignOnPage() {
  const say = useIntl().formatMessage;
  const failure = useFailure();
  const { email, org, next } = useSearch({ from: "/login/sso" });
  const find = $api.useMutation("post", "/api/v1/sign-in/sso");
  const asked = useRef(false);
  useEffect(() => {
    if (email && !asked.current) {
      asked.current = true;
      find.mutate({ body: { email, next } });
    }
  }, [email, next, find]);
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    find.mutate({ body: { email: value(new FormData(e.currentTarget), "email"), next } });
  };
  const found = find.data?.orgs;
  const offered = offeredFrom(found, org);
  const theOne = offered.length === 1 && offered[0]?.slug === org ? offered[0] : undefined;
  return (
    <SignedOutPage title={say(m.ssoTitle)}>
      <SignedOutTitle>
        {say(m.ssoTitle)}
        <span className="text-coral">{stop}</span>
      </SignedOutTitle>
      <Problem>{failure(find.error) ?? (found && found.length === 0 && say(m.noSSO))}</Problem>
      {theOne && <p className="text-sm text-muted-foreground">{say(v.asks, { org: theOne.name })}</p>}
      {offered.map((o) => (
        <ActionButton key={o.slug} size="lg" asChild>
          <a href={o.start_url}>{say(v.continueTo, { org: o.name })}</a>
        </ActionButton>
      ))}
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label={say(m.workEmail)} name="email" type="email" required autoComplete="username" defaultValue={email} />
        <ActionButton type="submit" quiet disabled={find.isPending}>
          {say(m.find)}
        </ActionButton>
      </form>
      <Link to="/login" search={{ next }} className={linkClass}>
        {say(m.passwordInstead)}
      </Link>
    </SignedOutPage>
  );
}

export function ChoosePasswordPage() {
  const say = useIntl().formatMessage;
  const failure = useFailure();
  const session = $api.useQuery("get", "/api/v1/session");
  const change = $api.useMutation("put", "/api/v1/password", { onSuccess: () => window.location.assign("/") });
  const signOut = $api.useMutation("delete", "/api/v1/session", { onSuccess: () => window.location.assign("/login") });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    change.mutate({ body: { current: value(f, "current"), new: value(f, "new") } });
  };
  return (
    <SignedOutPage title={say(m.chooseTitle)} framed={false}>
      <form onSubmit={submit} className="flex w-full flex-col gap-4 panel panel-lift p-7">
        <SignedOutTitle>{say(m.ownTitle)}</SignedOutTitle>
        <p className="text-sm text-muted-foreground">
          <FormattedMessage {...m.ownWhy} />
        </p>
        <Field label={say(m.given)} name="current" type="password" required autoComplete="current-password" />
        <Field label={say(m.newPassword)} name="new" type="password" required minLength={10} autoComplete="new-password" />
        <ActionButton type="submit" size="lg" disabled={change.isPending || change.isSuccess}>
          {say(m.continue)}
        </ActionButton>
        <Problem>{failure(change.error)}</Problem>
      </form>
      <div className="flex items-center gap-2.5 px-2.5 text-sm text-muted-foreground">
        <span className="flex-1 truncate">{session.data?.user.email}</span>
        <button type="button" className="text-xs hover:text-foreground" onClick={() => signOut.mutate({})}>
          {say(m.signOut)}
        </button>
      </div>
    </SignedOutPage>
  );
}
