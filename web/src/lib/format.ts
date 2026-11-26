// How pages write people, times and counts, as the server-rendered pages do.

export function actor(by: string) {
  return by.startsWith("agent:") ? "agent · " + by.slice(6) : by;
}

export function day(at: string) {
  return new Date(at).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

export function stamp(at: string) {
  const d = new Date(at);
  return day(at) + " " + d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// dated is a moment with its year, as "2 Jan 2006 15:04".
export function dated(at: string) {
  const d = new Date(at);
  const hm = d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
  return `${d.getDate()} ${months[d.getMonth()]} ${d.getFullYear()} ${hm}`;
}

export function plural(n: number, one: string, many = one + "s") {
  return `${n} ${n === 1 ? one : many}`;
}

export function clock(at: string) {
  return new Date(at).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}

// ago is how long since, as the server's pages say it: past a day, the date.
export function ago(at: string, now = Date.now()) {
  const ms = now - new Date(at).getTime();
  if (ms < 60_000) return "just now";
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)} min ago`;
  if (ms < 86_400_000) return plural(Math.floor(ms / 3_600_000), "hour") + " ago";
  return new Date(at).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}
