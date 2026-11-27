import { defineMessages } from "react-intl";
import { intl } from "@/i18n";
import type { components } from "@/api";

export type VendorKind = components["schemas"]["EnricherKind"];

// Every kind the spec knows, so a new one fails the build until it has a name here.
export const vendorNames: Record<VendorKind, string> = { hunter: "Hunter", prospeo: "Prospeo" };

// What each charges, as its own docs put it, for an owner setting a limit.
export const vendorCosts: Record<VendorKind, string> = {
  hunter: "Hunter charges a credit only when it finds an address.",
  prospeo:
    "Prospeo charges a credit for an email and ten for a mobile, only when it finds one, and nothing to look up the same person again within 90 days. Each lookup keeps only the detail it was for.",
};

// What each is sent about a person, matching its adapter.
export const vendorTold: Record<VendorKind, string> = {
  hunter: "Hunter is told each person's name and their company's domain.",
  prospeo: "Prospeo is told each person's name, their company's domain and any LinkedIn profile you hold.",
};

export const vendorKinds = Object.keys(vendorNames) as VendorKind[];

export function vendorName(kind: string) {
  return vendorNames[kind as VendorKind] ?? kind;
}

const details = defineMessages({
  email: { defaultMessage: "email", id: "aPrUFD", description: "A detail a vendor can find about a person: their email address, in the middle of a sentence" },
  phone: { defaultMessage: "mobile", id: "Cga/Te", description: "A detail a vendor can find about a person: their mobile number, in the middle of a sentence" },
  linkedin: {
    defaultMessage: "LinkedIn profile",
    id: "8UaEj7",
    description: "A detail a vendor can find about a person: their LinkedIn profile, in the middle of a sentence",
  },
});

// detailName is one of the details a vendor can find, as a person would say it.
export function detailName(field: string) {
  return field in details ? intl.formatMessage(details[field as keyof typeof details]) : field;
}

// month is the calendar month vendors' lookups are counted in, which is UTC's.
export function month(at = new Date()) {
  return intl.formatDate(at, { month: "long", timeZone: "UTC" });
}
