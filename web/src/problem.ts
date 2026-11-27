import type { components } from "./api.d.ts";

// problemText is what to tell someone a call failed with: an API problem's detail, the
// plain text a sign-in or same-site check answers with, or else the status's text, which
// HTTP/2 leaves empty. Empty is nothing to say, for the caller to word.
export function problemText(body: unknown, statusText: string): string {
  if (typeof body === "string" && body.trim() !== "") return body.trim();
  if (body && typeof body === "object" && "detail" in body && typeof body.detail === "string" && body.detail !== "") return body.detail;
  return statusText;
}

export type ProblemType = components["schemas"]["ProblemType"];

// problemType is the type of the Problem a call was answered with, if it got one.
export function problemType(error: unknown): ProblemType | undefined {
  if (typeof error !== "object" || error === null || !("type" in error) || typeof error.type !== "string") return undefined;
  return error.type as ProblemType;
}

// mustGoFirst is a refusal that loading the page sorts out: the server sends the
// browser where it has to go first.
export function mustGoFirst(error: unknown): boolean {
  const t = problemType(error);
  return t === "/api/problems/must_change_password" || t === "/api/problems/sso_required";
}

// retryQuery is React Query's retry: trying a throttled read again would only spend
// the rest of the caller's requests.
export function retryQuery(failures: number, error: unknown): boolean {
  return failures < 3 && problemType(error) !== "/api/problems/throttled";
}
