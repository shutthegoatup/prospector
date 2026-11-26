// safeHref is a link only to a web address: anything else that was stored, a javascript:
// one above all, links nowhere.
export function safeHref(u: string) {
  const t = u.trim();
  return /^https?:\/\/\S+$/i.test(t) ? t : "#";
}
