// problemText is what to tell someone a call failed with: an API problem's detail, the
// plain text a sign-in or same-site check answers with, or else the status.
export function problemText(body: unknown, statusText: string, status: number): string {
  if (typeof body === "string" && body.trim() !== "") return body.trim();
  if (body && typeof body === "object" && "detail" in body && typeof body.detail === "string" && body.detail !== "") return body.detail;
  return statusText || `The server answered ${status}.`;
}
