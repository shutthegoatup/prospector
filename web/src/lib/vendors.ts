import { defineMessage, defineMessages } from "react-intl";
import { intl } from "@/i18n";
import type { components } from "@/api";

export type VendorKind = components["schemas"]["EnricherKind"];

// Every kind the spec knows, so a new one fails the build until it has a name here.
export const vendorNames: Record<VendorKind, string> = { hunter: "Hunter", prospeo: "Prospeo" };

// What each charges, as its own docs put it, for an owner setting a limit.
const costs = {
  hunter: defineMessage<{ vendor: string }>({
    defaultMessage: "{vendor} charges a credit only when it finds an address.",
    id: "QvJCFf",
    description: "Under a data vendor's settings: what it charges. {vendor} is its name, Hunter",
  }),
  prospeo: defineMessage<{ vendor: string }>({
    defaultMessage:
      "{vendor} charges a credit for an email and ten for a mobile, only when it finds one, and nothing to look up the same person again within 90 days. Each lookup keeps only the detail it was for.",
    id: "lzZnZc",
    description: "Under a data vendor's settings: what it charges. {vendor} is its name, Prospeo",
  }),
} satisfies Record<VendorKind, unknown>;

// What each is sent about a person, matching its adapter.
const told = {
  hunter: defineMessage<{ vendor: string; linkedin: string }>({
    defaultMessage: "{vendor} is told each person's name and their company's domain.",
    id: "tzSPbm",
    description: "Under a data vendor's settings: what it is sent about a person. {vendor} is its name, Hunter",
  }),
  prospeo: defineMessage<{ vendor: string; linkedin: string }>({
    defaultMessage: "{vendor} is told each person's name, their company's domain and any {linkedin} profile you hold.",
    id: "NKZA6e",
    description: "Under a data vendor's settings: what it is sent about a person. {vendor} is its name, Prospeo; {linkedin} is LinkedIn",
  }),
} satisfies Record<VendorKind, unknown>;

export function vendorCosts(kind: VendorKind) {
  return intl.formatMessage(costs[kind], { vendor: vendorNames[kind] });
}

export function vendorTold(kind: VendorKind) {
  return intl.formatMessage(told[kind], { vendor: vendorNames[kind], linkedin: "LinkedIn" });
}

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
