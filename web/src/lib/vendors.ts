import type { components } from "@/api";

export type VendorKind = components["schemas"]["EnricherKind"];

// Every kind the spec knows, so a new one fails the build until it has a name here.
export const vendorNames: Record<VendorKind, string> = { hunter: "Hunter", prospeo: "Prospeo" };

// What each charges, as its own docs put it, for an owner setting a limit.
export const vendorCosts: Record<VendorKind, string> = {
  hunter: "Hunter charges a credit only when it finds an address.",
  prospeo: "Prospeo charges a credit for an email and ten for a mobile, only when it finds one, and nothing to look up the same person again within 90 days.",
};

export const vendorKinds = Object.keys(vendorNames) as VendorKind[];

export function vendorName(kind: string) {
  return vendorNames[kind as VendorKind] ?? kind;
}

// The details a vendor can find, as a person would say them.
export const detailNames: Record<string, string> = { email: "email", phone: "mobile", linkedin: "LinkedIn profile" };

// month is the calendar month vendors' lookups are counted in, which is UTC's.
export function month(at = new Date()) {
  return at.toLocaleDateString("en-GB", { month: "long", timeZone: "UTC" });
}
