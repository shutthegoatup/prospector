// withNext adds where to land after signing in to a page's address.
export function withNext(path: string, next: string | undefined): string {
  return next ? `${path}?${new URLSearchParams({ next })}` : path;
}

// offered are the organisations whose sign-on to show: only the one that sent someone
// here, when it is among them, as it asked for its own.
export function offered<T extends { slug: string }>(found: T[] | undefined, asked: string | undefined): T[] {
  const theOne = found?.find((o) => o.slug === asked);
  return theOne ? [theOne] : (found ?? []);
}

// failure is what to say when a call failed: an API problem's detail, the plain text a
// same-site check answers with, or, when nothing came back that says, otherwise.
export function failure(error: unknown, otherwise: string): string | undefined {
  if (error === null || error === undefined) return undefined;
  if (typeof error === "string" && error.trim() !== "") return error.trim();
  if (typeof error === "object" && "detail" in error && typeof error.detail === "string" && error.detail !== "") return error.detail;
  return otherwise;
}
