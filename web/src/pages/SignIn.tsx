import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useEffect, useRef, type FormEvent } from "react";
import { $api } from "@/client";
import { Field, Notice, Problem, SignedOutPage, SignedOutTitle } from "@/components/app/signed-out";
import { ActionButton } from "@/components/app";
import { safeNext } from "@/lib/next";
import { failure, offered as offeredFrom, withNext } from "@/lib/sign-in";

// After signing in the browser goes on as a page, not a route: the server decides where
// a session may go, from choosing a first password to an organisation's own sign-on.
function goOn(next: string | undefined) {
  window.location.assign(safeNext(next));
}

function value(f: FormData, name: string) {
  return String(f.get(name) ?? "");
}

const linkClass = "text-sm text-muted-foreground hover:text-foreground";

export function SignInPage() {
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
    <SignedOutPage title="Sign in">
      <h1 className="flex items-center gap-2.5 font-heading text-2xl font-bold">
        <img src="/assets/img/mark.svg" width="30" height="30" alt="" />
        Prospector.
      </h1>
      <Problem>{failure(signIn.error)}</Problem>
      {reset && !signIn.error && <Notice>Password saved. Sign in with it.</Notice>}
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Email" name="email" type="email" required autoComplete="username" />
        <Field label="Password" name="password" type="password" required autoComplete="current-password" />
        <ActionButton type="submit" size="lg" disabled={signIn.isPending || signIn.isSuccess}>
          Sign in
        </ActionButton>
        {w?.forgot && (
          <Link to="/forgot" className={linkClass}>
            Forgot your password?
          </Link>
        )}
      </form>
      {w && (w.social.length > 0 || w.sso) && (
        <nav className="flex flex-col gap-2 border-t border-dashed border-border pt-2" aria-label="Other ways to sign in">
          <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">Or</span>
          {w.social.map((way) => (
            <ActionButton key={way.key} quiet asChild>
              <a href={withNext(way.start_url, next)}>Continue with {way.label}</a>
            </ActionButton>
          ))}
          {w.sso && (
            <ActionButton quiet asChild>
              <Link to="/login/sso" search={{ next }}>
                Use your organisation's single sign-on
              </Link>
            </ActionButton>
          )}
        </nav>
      )}
    </SignedOutPage>
  );
}

export function ForgotPage() {
  const ask = $api.useMutation("post", "/api/v1/password-resets");
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    ask.mutate({ body: { email: value(new FormData(e.currentTarget), "email") } });
  };
  return (
    <SignedOutPage title="Reset your password">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>Reset your password</SignedOutTitle>
        {ask.isSuccess ? (
          <Notice>If that address has an account, a link is on its way. It lasts an hour.</Notice>
        ) : (
          <p className="text-sm text-muted-foreground">We'll email you a link to choose a new one.</p>
        )}
        <Problem>{failure(ask.error)}</Problem>
        <Field label="Email" name="email" type="email" required autoComplete="username" />
        <ActionButton type="submit" size="lg" disabled={ask.isPending}>
          Email me a link
        </ActionButton>
        <Link to="/login" className={linkClass}>
          Back to sign in
        </Link>
      </form>
    </SignedOutPage>
  );
}

export function ResetPage() {
  const { token } = useParams({ from: "/reset/$token" });
  // A dead link answers 410, which asking again won't change.
  const link = $api.useQuery("get", "/api/v1/password-resets/{token}", { params: { path: { token } } }, { retry: false });
  const save = $api.useMutation("post", "/api/v1/password-resets/{token}", { onSuccess: () => window.location.assign("/login?reset=1") });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    save.mutate({ params: { path: { token } }, body: { password: value(new FormData(e.currentTarget), "password") } });
  };
  return (
    <SignedOutPage title="Choose a password">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <SignedOutTitle>Choose a password</SignedOutTitle>
        {link.data && <p className="text-sm text-muted-foreground">For {link.data.email}. 10 characters or more.</p>}
        <Problem>{failure(link.error ?? save.error)}</Problem>
        <Field label="New password" name="password" type="password" required minLength={10} autoComplete="new-password" />
        <ActionButton type="submit" size="lg" disabled={save.isPending || save.isSuccess || !link.data}>
          Save password
        </ActionButton>
      </form>
    </SignedOutPage>
  );
}

// SingleSignOnPage only ever offers an organisation's sign-in as a link. Going there by
// itself would let a link from another site walk someone through a provider it chose.
export function SingleSignOnPage() {
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
    <SignedOutPage title="Single sign-on">
      <SignedOutTitle>
        Single sign-on<span className="text-coral">.</span>
      </SignedOutTitle>
      <Problem>
        {failure(find.error) ?? (found && found.length === 0 && "No organisation here uses single sign-on for that address. Sign in with your password instead.")}
      </Problem>
      {theOne && <p className="text-sm text-muted-foreground">{theOne.name} asks its people to sign in through its own provider.</p>}
      {offered.map((o) => (
        <ActionButton key={o.slug} size="lg" asChild>
          <a href={o.start_url}>Continue to {o.name}</a>
        </ActionButton>
      ))}
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Work email" name="email" type="email" required autoComplete="username" defaultValue={email} />
        <ActionButton type="submit" quiet disabled={find.isPending}>
          Find my organisation
        </ActionButton>
      </form>
      <Link to="/login" search={{ next }} className={linkClass}>
        Sign in with a password instead
      </Link>
    </SignedOutPage>
  );
}

export function ChoosePasswordPage() {
  const session = $api.useQuery("get", "/api/v1/session");
  const change = $api.useMutation("put", "/api/v1/password", { onSuccess: () => window.location.assign("/") });
  const signOut = $api.useMutation("delete", "/api/v1/session", { onSuccess: () => window.location.assign("/login") });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    change.mutate({ body: { current: value(f, "current"), new: value(f, "new") } });
  };
  return (
    <SignedOutPage title="Choose a password" framed={false}>
      <form onSubmit={submit} className="flex w-full flex-col gap-4 panel panel-lift p-7">
        <SignedOutTitle>Choose your own password</SignedOutTitle>
        <p className="text-sm text-muted-foreground">
          The one you signed in with was shown to whoever added you. Choose one only you know, 10 characters or more.
        </p>
        <Field label="Password you were given" name="current" type="password" required autoComplete="current-password" />
        <Field label="New password" name="new" type="password" required minLength={10} autoComplete="new-password" />
        <ActionButton type="submit" size="lg" disabled={change.isPending || change.isSuccess}>
          Continue
        </ActionButton>
        <Problem>{failure(change.error)}</Problem>
      </form>
      <div className="flex items-center gap-2.5 px-2.5 text-sm text-muted-foreground">
        <span className="flex-1 truncate">{session.data?.user.email}</span>
        <button type="button" className="text-xs hover:text-foreground" onClick={() => signOut.mutate({})}>
          Sign out
        </button>
      </div>
    </SignedOutPage>
  );
}
