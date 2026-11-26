import type { components } from "@/api";

export type VendorKind = components["schemas"]["EnricherKind"];

// Every kind the spec knows, so a new one fails the build until it has a name here.
export const vendorNames: Record<VendorKind, string> = { hunter: "Hunter" };

export const vendorKinds = Object.keys(vendorNames) as VendorKind[];

export function vendorName(kind: string) {
  return vendorNames[kind as VendorKind] ?? kind;
}

// month is the calendar month vendors' lookups are counted in, which is UTC's.
export function month(at = new Date()) {
  return at.toLocaleDateString("en-GB", { month: "long", timeZone: "UTC" });
}
