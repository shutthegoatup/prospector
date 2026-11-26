// safeNext keeps where to go after signing in on this site, as the server's safeNext
// does. Browsers drop tabs and newlines from an address and read \ as /, so either
// could turn a path into //another.host.
export function safeNext(next: string | undefined): string {
  // oxlint-disable-next-line no-control-regex -- control characters are exactly what an address to go on to must not have
  if (!next || !next.startsWith("/") || next.startsWith("//") || /[\u0000-\u001f\u007f\\]/.test(next)) return "/";
  return next;
}
