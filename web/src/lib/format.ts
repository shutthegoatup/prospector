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

export function plural(n: number, one: string, many = one + "s") {
  return `${n} ${n === 1 ? one : many}`;
}
