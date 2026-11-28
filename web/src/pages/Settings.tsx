import { Link } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useState } from "react";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, Pill, Section, useRefresh } from "@/components/app";
import { Bar, Placeholders, Text } from "@/components/app/skeleton";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { intl } from "@/i18n";
import { actor } from "@/lib/format";
import { writeClipboard } from "@/lib/clipboard";
import { cn } from "@/lib/utils";
import { useSession } from "@/session";
import locales from "../../locales.json";

type Settings = components["schemas"]["Settings"];
type Me = components["schemas"]["Me"];
type Member = components["schemas"]["Member"];
type MailServer = components["schemas"]["MailServer"];
type SSO = components["schemas"]["SSO"];
type Suppression = components["schemas"]["Suppression"];
type Offering = components["schemas"]["Offering"];

// Names, and what the server reads in the organisation's own text, are the same in every language.
const prospector = "Prospector";
const linkedIn = "LinkedIn";
const first = "{first}";
const company = "{company}";
const orgToken = "{org}";
const must = "(must)";
const them = "them:";
const us = "us:";
const providers = "Okta, Entra ID, Google Workspace";
const oidc = "OpenID Connect";
const prospectorURL = "PROSPECTOR_URL";
const secretKeys = "PROSPECTOR_SECRET_KEYS";
const secretKeyCommand = "`prospector secret-key`";
const dot = " · ";
const down = "↓ ";
const chevron = "›";
const smtpExample = "smtp.example.com";
const siteExample = "https://www.example.com";
const issuerExample = "https://login.example.com";
const domainExample = "example.com";
const addressExample = "someone@example.com";
const space = " ";
const senderExample = (org: string) => `${org} <hello@example.com>`;

const m = defineMessages({
  settings: { defaultMessage: "Settings", id: "tck50S", description: "Title and heading of the settings page" },
  saved: { defaultMessage: "Saved ✓", id: "GMAXIB", description: "On the settings page beside a Save button once what was entered is saved" },
  save: { defaultMessage: "Save", id: "JZ4bFJ", description: "Button on the settings page to save a section's fields" },
  add: { defaultMessage: "Add", id: "8OWYeB", description: "Button on the settings page to add what was entered (an offering, an address, a member)" },
  cancel: { defaultMessage: "Cancel", id: "xyEyxi", description: "Button on the settings page to stop adding an offering" },
  remove: { defaultMessage: "Remove", id: "ynGBDI", description: "Button on the settings page to remove an offering, a domain or a member" },
  name: {
    defaultMessage: "Name",
    id: "7hSKJy",
    description: "Label of a name field on the settings page (the organisation's, an offering's or a new member's)",
  },
  agents: { defaultMessage: "Agents", id: "hrfaXz", description: "Heading of the settings page's section pointing to the agents page" },
  thisOrganisation: {
    defaultMessage: "this organisation",
    id: "S03xMe",
    description: "Stands in for the organisation's name in the settings page's questions before removing a member, when the name hasn't loaded",
  },
  voice: { defaultMessage: "Your voice", id: "thSICP", description: "Heading of the settings page's section for the member's caller name and signature" },
  voiceHint: {
    defaultMessage: "Yours in this organisation: used in call scripts and added to copied emails.",
    id: "zdoQyq",
    description: "Under the heading of the settings page's Your voice section",
  },
  callerName: { defaultMessage: "Caller name", id: "zrp8FV", description: "Label of the field for the name the member gives in call scripts" },
  signature: { defaultMessage: "Signature", id: "egip4r", description: "Label of the field for the member's email signature" },
  language: { defaultMessage: "Your language", id: "sJ95xa", description: "Heading of the settings page's section for the member's own language" },
  languageHint: {
    defaultMessage: "For you in every organisation: the app, mail sent to you, and the server's messages.",
    id: "+5rSoX",
    description: "Under the heading of the settings page's Your language section",
  },
  languageLabel: { defaultMessage: "Language", id: "utQjdQ", description: "Label of the list of languages the member can choose" },
  browsers: {
    defaultMessage: "Your browser's",
    id: "IhvJqO",
    description: "In the list of languages: the choice to use the browser's languages, when another is chosen",
  },
  browsersUsed: {
    defaultMessage: "Your browser's languages are used until you choose one.",
    id: "cM8+sS",
    description: "Under the list of languages on the settings page",
  },
  signInTitle: {
    defaultMessage: "Your sign-in",
    id: "gbVUXI",
    description: "Heading of the settings page's section for a member who signs in only through a provider",
  },
  noPassword: {
    defaultMessage: "You sign in through a provider, so there is no password here to change. Whoever runs this server can give you one.",
    id: "tt+6T2",
    description: "Under the heading of the Your sign-in section, for a member with no password",
  },
  password: { defaultMessage: "Your password", id: "nlb6lk", description: "Heading of the settings page's section to change the member's password" },
  passwordHint: {
    defaultMessage: "For your sign-in across every organisation. Changing it signs out your other sessions.",
    id: "2skwfE",
    description: "Under the heading of the settings page's Your password section",
  },
  current: { defaultMessage: "Current", id: "x9OsPh", description: "Label of the field for the member's current password" },
  newPassword: { defaultMessage: "New", id: "sTSLFC", description: "Label of the field for the member's new password" },
  change: { defaultMessage: "Change", id: "ySGBRg", description: "Button to change the member's password" },
  neverHint: {
    defaultMessage:
      "One per line: a domain, an email address or a LinkedIn profile link. Applied to everything people and agents add, and to who comes up for outreach on Today.",
    id: "04CRgW",
    description: "Under the organisation's never-contact list on the settings page. Today is the name of a page",
  },
  organisation: { defaultMessage: "Organisation", id: "H7VkNM", description: "Heading of the settings page's section of the organisation's settings" },
  shared: {
    defaultMessage: "Shared by everyone in this organisation.",
    id: "AIRKlG",
    description: "Under the heading of the Organisation section, for an owner",
  },
  ownerOnly: {
    defaultMessage: "Only an owner can change these.",
    id: "jQT5bj",
    description: "Under the heading of the Organisation section, for a member who isn't an owner",
  },
  colour: { defaultMessage: "Colour", id: "xayQrS", description: "Label of the organisation's colour picker" },
  customerLanguage: { defaultMessage: "Language for customers", id: "OGVI+1", description: "Label of the list of languages for the organisation's customers" },
  customerLanguageHint: {
    defaultMessage: "Customers' pages, unsubscribe pages, and mail to customers and new members, for anyone who hasn't chosen.",
    id: "RWF+3h",
    description: "Under the list of languages for customers: what it is used for",
  },
  emailAfter: {
    defaultMessage: "Email after",
    id: "BqxUi/",
    description: "Label of the number of days before emailing someone who hasn't accepted on LinkedIn",
  },
  followUpAfter: { defaultMessage: "Follow up after", id: "jbdgLM", description: "Label of the number of days before following up an email with no reply" },
  connectionNote: {
    defaultMessage: "Connection note",
    id: "/SqyDK",
    description: "Label of the template for the note sent with a LinkedIn connection request",
  },
  whatYouDo: { defaultMessage: "What you do", id: "aMRUY/", description: "Label of the pitch line used in call scripts" },
  neverContact: { defaultMessage: "Never contact", id: "5WFbSP", description: "Label of the organisation's list of who is never to be contacted" },
  neverPlaceholder: {
    defaultMessage: "domains, emails or LinkedIn links, one per line",
    id: "mGQucr",
    description: "Placeholder in the empty never-contact list",
  },
  lookFor: { defaultMessage: "What you look for", id: "3zNZWr", description: "Label of the list of what agents check a company for" },
  lookForPlaceholder: {
    defaultMessage: "careers_page: a careers or jobs page on their site\nats: the applicant tracking system they use",
    id: "Omlvk6",
    description:
      "Example in the empty list of what agents look for: one per line, a key name, a colon and what counts. Keep the key names (careers_page, ats) as they are",
  },
  agentGuidance: { defaultMessage: "Agent guidance", id: "WpORzw", description: "Label of the organisation's own guidance for agents" },
  agentGuidancePlaceholder: {
    defaultMessage: "Who you sell to, who's off-limits, how to judge fit",
    id: "rj/2HQ",
    description: "Placeholder in the empty agent guidance field",
  },
  agentGuidanceHint: {
    defaultMessage: "Sent to every agent when it connects, after the built-in task guidance, and returned by the guidance tool.",
    id: "srxgTh",
    description: "Under the agent guidance field",
  },
  enquirySites: { defaultMessage: "Enquiry form sites", id: "Zon+75", description: "Label of the list of websites whose forms may send enquiries" },
  onboardingSteps: { defaultMessage: "Onboarding steps", id: "yYDIWz", description: "Label of the list of steps a new customer goes through" },
  currency: { defaultMessage: "Currency", id: "hrbyYj", description: "Label of the currency deals are valued in" },
  currencyHint: {
    defaultMessage: "The three-letter code deals are valued in, such as GBP, EUR or USD. Changing it leaves the deals you already valued as they are.",
    id: "y3bGFr",
    description: "Under the currency field",
  },
  postalAddress: { defaultMessage: "Postal address", id: "uHGc+K", description: "Label of the organisation's postal address" },
  postalPlaceholder: {
    defaultMessage: "Alpha Ltd\n1 Example Street\nLeith EH6 1AA",
    id: "sFgaZZ",
    description: "Example in the empty postal address field: an invented company's address, on three lines",
  },
  postalHint: {
    defaultMessage:
      "Where you can be written to. It goes at the foot of every campaign, as the law on marketing mail expects, and a campaign can't be sent without it.",
    id: "vQy8ku",
    description: "Under the postal address field",
  },
  coldEmail: { defaultMessage: "Cold email", id: "v8Bnc5", description: "Label of the setting for which companies may be emailed cold" },
  incorporatedOnly: {
    defaultMessage: "Only email incorporated companies (UK PECR)",
    id: "+s4tcJ",
    description: "Checkbox: email only incorporated companies, as the UK's Privacy and Electronic Communications Regulations allow",
  },
  mailOwn: {
    defaultMessage: "This organisation's own mail server. Invitations to new members leave through it.",
    id: "09m07+",
    description: "Under the heading of the Outgoing mail section, when the organisation has a mail server",
  },
  mailSite: {
    defaultMessage: "This organisation's own mail server. Invitations to new members leave through it; without one, they come from this site's address.",
    id: "vLQORx",
    description: "Under the heading of the Outgoing mail section, when the organisation has none but the site can send mail",
  },
  mailNone: {
    defaultMessage:
      "This organisation's own mail server. Invitations to new members leave through it; without one, a new member's first password is shown to you instead.",
    id: "/vLmGD",
    description: "Under the heading of the Outgoing mail section, when neither the organisation nor the site can send mail",
  },
  bounces: {
    defaultMessage: "Bounces and spam complaints",
    id: "eklRVU",
    description: "Heading of the settings section that connects Postmark's reports of mail that came back",
  },
  bouncesOff: { defaultMessage: "Postmark isn't reporting here yet.", id: "70Htj2", description: "In the Bounces section, before Postmark is connected" },
  bouncesOffHint: { defaultMessage: "For the Postmark server your mail goes through.", id: "ieSwqc", description: "Under the Connect Postmark button" },
  connectPostmark: {
    defaultMessage: "Connect Postmark",
    id: "oWRzxG",
    description: "Button making the address Postmark reports bounces and spam complaints to",
  },
  bouncesOn: {
    defaultMessage: "Postmark reports bounces and spam complaints here.",
    id: "NwFzBG",
    description: "In the Bounces section, once Postmark is connected",
  },
  newHookAddress: { defaultMessage: "Make a new address", id: "hp9sm0", description: "Button replacing the address Postmark reports to, as when it was lost" },
  newHookHint: {
    defaultMessage: "A new address replaces the one Postmark has, so paste it there too.",
    id: "R1ZNJ9",
    description: "Under the Make a new address button",
  },
  stopBounces: { defaultMessage: "Stop", id: "xkH+Dm", description: "Button to stop taking bounces and spam complaints from Postmark" },
  stopBouncesAsk: {
    defaultMessage: "Stop taking bounces and spam complaints from Postmark? Addresses already listed stay listed.",
    id: "iAm3Gz",
    description: "Asked before stopping Postmark's reports",
  },
  hookShownOnce: {
    defaultMessage: "The address holds its password and is shown only now. If you lose it, connect again: the old one stops working a day later.",
    id: "GhgLQQ",

    description: "Under the address to paste into Postmark",
  },
  copyHook: { defaultMessage: "Copy", id: "JVE/Sg", description: "Button copying the address to paste into Postmark" },
  copiedHook: { defaultMessage: "Copied ✓", id: "bj8fFc", description: "The copy button's label once it has copied the address" },
  hookAddress: { defaultMessage: "Address for Postmark", id: "tiOCmO", description: "What a screen reader calls the address to paste into Postmark" },
  outgoingMail: { defaultMessage: "Outgoing mail", id: "JZ9UqU", description: "Heading of the settings page's section for the organisation's mail server" },
  server: { defaultMessage: "Server", id: "+OCmsc", description: "Label of the mail server's host name" },
  port: { defaultMessage: "Port", id: "mXf2h4", description: "What a screen reader calls the mail server's port field" },
  serverHint: {
    defaultMessage: "SMTP over TLS, on port 587, 465, 2525 or 25. Clear the server to stop using it.",
    id: "hdYGED",
    description: "Under the mail server's host and port",
  },
  username: { defaultMessage: "Username", id: "dVtPYu", description: "Label of the mail server's username" },
  mailPassword: { defaultMessage: "Password", id: "or6Zzo", description: "Label of the mail server's password" },
  keepPassword: {
    defaultMessage: "saved: leave empty to keep it",
    id: "EDoHbY",
    description: "Placeholder in the mail server's password field when one is already saved",
  },
  passwordNeverShown: {
    defaultMessage: "Never shown again. Changing the server or username needs it entered again.",
    id: "BBjN4Z",
    description: "Under the mail server's password field",
  },
  from: { defaultMessage: "From", id: "U40Ry9", description: "Label of the address the organisation's mail is sent from" },
  noMailServer: {
    defaultMessage: "No mail server of this organisation's own.",
    id: "Av0sf/",
    description: "After saving the Outgoing mail section with the server cleared",
  },
  testEmail: { defaultMessage: "Send me a test email", id: "Sg9anX", description: "Button to send the member a test email through the mail server" },
  sequence: { defaultMessage: "Sequence", id: "wdfcXc", description: "Heading of the settings page's section showing the steps every contact goes through" },
  sequenceHint: {
    defaultMessage: "Every contact goes through these steps; the waits come from above.",
    id: "EtAwvM",
    description: "Under the heading of the Sequence section; the waits are the day counts set in the Organisation section",
  },
  stepRequest: { defaultMessage: "LinkedIn connection request", id: "cB+Ak2", description: "First step of the outreach sequence" },
  stepMessage: { defaultMessage: "LinkedIn message if accepted, else email", id: "0sf5ei", description: "Second step of the outreach sequence" },
  stepFollowUp: { defaultMessage: "Follow-up email", id: "5fEbck", description: "Third step of the outreach sequence" },
  waitCall: {
    defaultMessage: "whenever a number is known, in office hours",
    id: "Jvx1by",
    description: "In the outreach sequence: when the call after the follow-up email comes",
  },
  stepCall: { defaultMessage: "Call", id: "phA2AA", description: "Last step of the outreach sequence: a phone call" },
  offeringPlaceholder: { defaultMessage: "Contract roles", id: "j4Facc", description: "Example in the empty name field of a new offering" },
  offeringLookForPlaceholder: {
    defaultMessage: "ir35: outside IR35, or not stated\nopen_roles (must): a contract role open now",
    id: "haFNx+",
    description:
      "Example in an offering's empty list of what agents look for: one per line, a key name, a colon and what counts. Keep the key names (ir35, open_roles) and (must) as they are",
  },
  offeringLookForHint: {
    defaultMessage: "As above, one per line. Agents on a task for this offering check these in place of the organisation's own.",
    id: "85X/7s",
    description: "Under an offering's list of what agents look for",
  },
  offeringPitchHint: {
    defaultMessage: "The call script's pitch line for people reached for it. Empty uses the organisation's.",
    id: "28uPqG",
    description: "Under an offering's pitch line",
  },
  whatYouSell: { defaultMessage: "What you sell", id: "oS/mDq", description: "Heading of the settings page's section of the organisation's offerings" },
  addOffering: { defaultMessage: "+ Add an offering", id: "sydTE9", description: "Button to start adding an offering" },
  nothingToLookFor: {
    defaultMessage: "nothing to look for yet",
    id: "fBbtX5",
    description: "In an offering's summary when it has nothing for agents to look for",
  },
  edit: { defaultMessage: "Edit", id: "fjaFow", description: "Beside an offering's summary, to open it for an owner to edit" },
  notToBeMailed: {
    defaultMessage: "Not to be mailed",
    id: "yDfRQA",
    description: "Heading of the settings page's section of addresses marketing mail never goes to",
  },
  notToBeMailedHint: {
    defaultMessage:
      "Marketing mail never goes to these addresses. Someone who unsubscribes by the link in a mail lands here and stays: only they can undo it. Your never-contact list and anyone erased are kept out as well.",
    id: "0JAmOt",
    description: "Under the heading of the Not to be mailed section",
  },
  nobody: { defaultMessage: "Nobody yet.", id: "TQjxn6", description: "In the Not to be mailed section when no address is in it" },
  takeOff: { defaultMessage: "Take off", id: "BEgIPa", description: "Button to take an address off the list not to be mailed" },
  newestShown: {
    defaultMessage: "The newest are shown. The API lists them all.",
    id: "pM75YV",
    description: "Under the list not to be mailed when there are more than it shows",
  },
  addressNotToMail: {
    defaultMessage: "Address not to mail",
    id: "Y3YflR",
    description: "What a screen reader calls the field to add an address not to be mailed",
  },
  singleSignOn: { defaultMessage: "Single sign-on", id: "gAprNj", description: "Heading of the settings page's section for the organisation's single sign-on" },
  yourDomains: { defaultMessage: "Your domains", id: "xz+PoA", description: "Heading of the list of email domains single sign-on covers" },
  verified: { defaultMessage: "verified", id: "738jqv", description: "Tag beside a domain proved to be the organisation's" },
  notVerified: { defaultMessage: "not verified", id: "9fpcTu", description: "Tag beside a domain not yet proved to be the organisation's" },
  verify: { defaultMessage: "Verify", id: "rZnhyL", description: "Button to check a domain's TXT record" },
  emailDomain: { defaultMessage: "Email domain", id: "5iPRKh", description: "What a screen reader calls the field to add a domain to single sign-on" },
  addDomain: { defaultMessage: "Add domain", id: "WexFNb", description: "Button to add a domain to single sign-on" },
  domainsHint: {
    defaultMessage: "Single sign-on only ever speaks for addresses at a verified domain, matched whole: example.com doesn't cover mail.example.com.",
    id: "pJqWwT",
    description: "Under the list of single sign-on domains",
  },
  issuer: { defaultMessage: "Issuer", id: "Lqpmzy", description: "Label of the single sign-on provider's issuer address" },
  clientID: { defaultMessage: "Client ID", id: "iYWqCK", description: "Label of the client ID the provider gave for single sign-on" },
  clientSecret: { defaultMessage: "Client secret", id: "GGhAaz", description: "Label of the client secret the provider gave for single sign-on" },
  keptSecret: {
    defaultMessage: "Kept. Type a new one to replace it",
    id: "eQ0Zc+",
    description: "Placeholder in the client secret field when one is already saved",
  },
  redirectURI: { defaultMessage: "Redirect URI", id: "HgOAJ9", description: "Label of the address the provider sends people back to" },
  redirectHint: {
    defaultMessage: "Register this with your provider as the place to send people back.",
    id: "0V2j1C",
    description: "Under the redirect URI",
  },
  requireIt: { defaultMessage: "Require it", id: "HEFVLy", description: "Label of the setting that makes single sign-on the only way in" },
  mustUse: { defaultMessage: "Members at your domains must sign in this way", id: "2PojBj", description: "Checkbox to require single sign-on" },
  requireHint: {
    defaultMessage: "Owners can still use a password, so a broken provider can be put right. API keys are not affected.",
    id: "xrOVGR",
    description: "Under the checkbox to require single sign-on",
  },
  newPeople: {
    defaultMessage: "New people",
    id: "Hb6Gx/",
    description: "Label of the setting for people who sign in through single sign-on before being added",
  },
  autoJoin: {
    defaultMessage: "Anyone at your domains becomes a member when they first sign in",
    id: "d4OLaK",
    description: "Checkbox to let anyone at the organisation's domains join by single sign-on",
  },
  autoJoinHint: {
    defaultMessage: "Never an owner. Someone you remove here joins again while your provider still lets them in: remove them there too.",
    id: "uXvX1U",
    description: "Under the checkbox to let people join by single sign-on",
  },
  owner: { defaultMessage: "owner", id: "jfr19z", description: "A member's role in the team list: an owner of the organisation" },
  readsOnly: { defaultMessage: "member · reads only", id: "3Y2DHq", description: "A member's role in the team list: a member with no area of work" },
  makeOwner: { defaultMessage: "Make owner", id: "nu9rv/", description: "Button to make a member an owner" },
  makeMember: { defaultMessage: "Make member", id: "hldmdb", description: "Button to make an owner a member" },
  team: { defaultMessage: "Team", id: "UtYZXR", description: "Heading of the settings page's section of the organisation's members" },
  email: { defaultMessage: "Email", id: "XiVUuZ", description: "What a screen reader calls the field for a new member's email address" },
  emailPlaceholder: { defaultMessage: "email", id: "TYXrTP", description: "Placeholder in the field for a new member's email address" },
  namePlaceholder: { defaultMessage: "name", id: "9y7E+h", description: "Placeholder in the field for a new member's name" },
  role: { defaultMessage: "Role", id: "sLQ/yo", description: "What a screen reader calls the list of roles for a new member" },
  roleMember: { defaultMessage: "member", id: "+rrVn7", description: "In the list of roles for a new member: a member" },
  roleOwner: { defaultMessage: "owner", id: "CA5NWe", description: "In the list of roles for a new member: an owner" },
  sales: { defaultMessage: "sales", id: "fHJIkM", description: "An area of work a member can be given: sales" },
  salesWhat: {
    defaultMessage: "research, outreach, deals and tasks for agents",
    id: "efMHa5",
    description: "Tooltip on the sales area of work: what it covers",
  },
  support: { defaultMessage: "support", id: "WjhXAJ", description: "An area of work a member can be given: support" },
  supportWhat: {
    defaultMessage: "enquiries and their replies, subscriptions and onboarding",
    id: "XTJdw6",
    description: "Tooltip on the support area of work: what it covers",
  },
  yourData: { defaultMessage: "Your data", id: "sLEPn0", description: "Heading of the settings page's section to export the organisation's data" },
  yourDataHint: {
    defaultMessage:
      "Everything this organisation holds here, in one file: companies, people, facts, enquiries, history, settings and who has access. It holds no passwords, keys or sign-on secrets.",
    id: "QIaU9I",
    description: "In the Your data section, above the button to download it",
  },
  download: { defaultMessage: "Download everything", id: "EsfgKc", description: "Button to download everything the organisation holds as one file" },
});

const v = {
  browsersIs: defineMessage<{ language: string }>({
    defaultMessage: "Your browser's ({language})",
    id: "Ewp4ZD",
    description:
      "In the list of languages, when none is chosen: the choice to use the browser's languages. {language} is the one they came to, named in itself",
  }),
  neverCovers: defineMessage<{ total: number; shown: string; more: number }>({
    defaultMessage:
      "It currently keeps {total, plural, one {# person} other {# people}} already here out of the queue: {shown}{more, plural, =0 {} one { and # more} other { and # more}}.",
    id: "U+hYJs",
    description:
      "After the hint under the never-contact list. {total} is how many people it keeps out of outreach, {shown} a list of some of their names or addresses, {more} how many of them aren't listed",
  }),
  noteHint: defineMessage<{ first: string; company: string; org: string; linkedIn: string }>({
    defaultMessage: "{first} {company} {org} are filled in. {linkedIn} allows 300 characters.",
    id: "p6NCUa",
    description:
      "Under the connection note template. {first}, {company} and {org} are the placeholders a note can hold, written as they are typed; {linkedIn} is the network's name",
  }),
  pitchHint: defineMessage<{ company: string }>({
    defaultMessage: "The call script's pitch line. {company} is filled in.",
    id: "gV/1o5",
    description: "Under the pitch line. {company} is the placeholder it can hold, written as it is typed",
  }),
  offeringNoteHint: defineMessage<{ first: string; company: string; org: string }>({
    defaultMessage: "For people picked for it. Empty uses the organisation's. {first} {company} {org} are filled in.",
    id: "uJLSGT",
    description: "Under an offering's connection note. {first}, {company} and {org} are the placeholders a note can hold, written as they are typed",
  }),
  lookForHint: defineMessage<{ must: string }>({
    defaultMessage:
      "One per line, a name and what counts; put {must} after the name of anything a company is no fit without. Agents answer each yes, no or unknown with a source, and Research ranks a search's companies by them.",
    id: "PXPfYy",
    description: "Under the list of what agents look for. {must} is the mark typed after a name, written as it is typed; Research is the name of a page",
  }),
  enquiryHint: defineMessage<{ intake: string }>({
    defaultMessage: "One per line. A form on these sites can POST an enquiry as JSON to {intake} and it shows under Enquiries. Leave empty to accept none.",
    id: "I/YwtY",
    description: "Under the list of enquiry form sites. {intake} is the address forms send to; Enquiries is the name of a page",
  }),
  onboardingPlaceholder: defineMessage<{ them: string; us: string }>({
    defaultMessage: "{them} Name a project lead, 2 days\n{them} Send your product files, 7 days\n{us} Kick-off call, 10 days\n{us} Go live, 30 days",
    id: "p5zbja",
    description:
      "Example in the empty list of onboarding steps: one step a line, who does it ({them} the customer, {us} the organisation, written as they are typed), the step, and the days after the start it is due",
  }),
  onboardingHint: defineMessage<{ them: string; us: string }>({
    defaultMessage:
      "What a new customer goes through, one step a line, in order. Start a line with {them} or {us} for who does it ({us} when left out), and end it with a number of days after the start when it is due. A customer counts as live once every step is done. Changing these leaves onboardings already running as they are.",
    id: "lY9vNW",
    description: "Under the list of onboarding steps. {them} (the customer) and {us} (the organisation) are written as they are typed",
  }),
  waitAccept: defineMessage<{ n: number }>({
    defaultMessage: "on accept, or after {n, plural, one {# day} other {# days}}",
    id: "qFJpSH",
    description: "In the outreach sequence: when the step after a LinkedIn connection request comes. {n} is the days set in the Organisation section",
  }),
  waitReply: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# day} other {# days}} without a reply",
    id: "AU7RdN",
    description: "In the outreach sequence: when the follow-up email comes. {n} is the days set in the Organisation section",
  }),
  bounced: defineMessage<{ date: string }>({
    defaultMessage: "bounced {date}",
    id: "1IerLu",
    description: "Beside an address not to be mailed: when mail to it bounced. {date} is the day",
  }),
  unsubscribed: defineMessage<{ date: string }>({
    defaultMessage: "unsubscribed {date}",
    id: "Y5XwNY",
    description: "Beside an address not to be mailed: when its owner unsubscribed. {date} is the day",
  }),
  complained: defineMessage<{ date: string }>({
    defaultMessage: "marked a mail as spam {date}",
    id: "R4Ycdt",
    description: "Beside an address not to be mailed: when its owner marked a mail as spam. {date} is the day",
  }),
  addedBy: defineMessage<{ who: string; date: string }>({
    defaultMessage: "added by {who} {date}",
    id: "g1rSyq",
    description: "Beside an address not to be mailed: who added it and when. {who} is a member or an agent, {date} the day",
  }),
  whyAndBounced: defineMessage<{ why: string; bounced: string }>({
    defaultMessage: "{why} · {bounced}",
    id: "pV6gaG",
    description: "Beside an address not to be mailed: why it is there, then when mail to it bounced",
  }),
  whatYouSellHint: defineMessage<{ org: string }>({
    defaultMessage:
      "Everything above is {org}'s own offering. Add another when you sell something else to companies you already know: each one is reviewed for on its own, with its own look-for fields, pitch and note, while facts and people are shared.",
    id: "WhA6gv",
    description: "Under the heading of the What you sell section. {org} is the organisation's name",
  }),
  onlyOwn: defineMessage<{ org: string }>({
    defaultMessage: "Only {org}'s own.",
    id: "Z8qXFm",
    description: "In the What you sell section when the organisation sells nothing but its own offering. {org} is its name",
  }),
  moreFields: defineMessage<{ n: number }>({
    defaultMessage: "+{n} more",
    id: "SXjjXI",
    description: "At the end of an offering's summary of what agents look for: how many more aren't listed",
  }),
  removeOffering: defineMessage<{ name: string }>({
    defaultMessage:
      "Remove {name}? Where companies stand for it is forgotten, though their history keeps it, and people reached for it are then reached for the organisation's own.",
    id: "JMroJ4",
    description: "Asked before removing an offering. {name} is its name",
  }),
  changedBy: defineMessage<{ who: string }>({
    defaultMessage: "Changed by {who}",
    id: "LSGLou",
    description: "Under an offering: who last changed it. {who} is a member or an agent",
  }),
  bouncesHint: defineMessage<{ prospector: string }>({
    defaultMessage:
      "When mail to an address bounces for good, or someone marks it as spam, Postmark tells {prospector}, and the address goes on Not to be mailed. Soft bounces change nothing.",
    id: "F2/5Ij",
    description: "Under the heading of the Bounces section. {prospector} is the product's name",
  }),
  mailAgainBounced: defineMessage<{ address: string; date: string }>({
    defaultMessage: "Mail {address} again? It bounced on {date}; if it still bounces, Postmark puts it back.",
    id: "Et85CB",
    description: "Asked before taking a bounced address off the list not to be mailed. {address} is the address, {date} when it bounced",
  }),
  mailAgain: defineMessage<{ address: string }>({
    defaultMessage: "Mail {address} again?",
    id: "MpQv4v",
    description: "Asked before taking an address off the list not to be mailed. {address} is the address",
  }),
  setURL: defineMessage<{ variable: string }>({
    defaultMessage: "Set {variable} to this app's address first: it is where the provider sends people back.",
    id: "3CZtXe",
    description: "In the Single sign-on section when the server doesn't know its own address. {variable} is the setting's name",
  }),
  ssoHint: defineMessage<{ providers: string; oidc: string }>({
    defaultMessage: "Let people at your company's email domains sign in through your own provider: {providers} or anything else that speaks {oidc}.",
    id: "7aLALl",
    description: "Under the heading of the Single sign-on section. {providers} names some providers, {oidc} the standard",
  }),
  removeDomain: defineMessage<{ domain: string }>({
    defaultMessage: "Remove {domain}? People at it can no longer use single sign-on here.",
    id: "J5zEd+",
    description: "Asked before removing a domain from single sign-on. {domain} is the domain",
  }),
  issuerHint: defineMessage<{ oidc: string }>({
    defaultMessage: "Your provider's {oidc} issuer. Empty turns single sign-on off.",
    id: "T1eJzT",
    description: "Under the single sign-on issuer field. {oidc} is the standard's name",
  }),
  sealing: defineMessage<{ variable: string; command: string }>({
    defaultMessage:
      "A secret is kept sealed, so none can be saved until whoever runs this server sets {variable} ({command} makes one). Everything else here can still be changed.",
    id: "CsCbwg",
    description:
      "Under the client secret field when the server has no key to seal it with. {variable} is the setting's name, {command} the command that makes a key",
  }),
  memberOf: defineMessage<{ areas: string }>({
    defaultMessage: "member · {areas}",
    id: "uKaloo",
    description: "A member's role in the team list, with the areas of work they have (such as sales, support)",
  }),
  makeOwnerConfirm: defineMessage<{ email: string; org: string }>({
    defaultMessage: "Make {email} an owner of {org}? They can change settings, keys and the team, export and erase.",
    id: "R6X9sc",
    description: "Asked before making a member an owner. {email} is theirs, {org} the organisation's name",
  }),
  stopOwnerConfirm: defineMessage<{ org: string }>({
    defaultMessage: "Stop being an owner of {org}? Only another owner can make you one again.",
    id: "vpHsZz",
    description: "Asked before an owner makes themselves a member. {org} is the organisation's name",
  }),
  makeMemberConfirm: defineMessage<{ email: string }>({
    defaultMessage: "Make {email} a member? They stop being able to change settings, keys and the team.",
    id: "8Zt7LO",
    description: "Asked before making an owner a member. {email} is theirs",
  }),
  leaveConfirm: defineMessage<{ org: string }>({
    defaultMessage: "Leave {org}? You lose access to it.",
    id: "3bcpLQ",
    description: "Asked before a member removes themselves. {org} is the organisation's name",
  }),
  removeMemberConfirm: defineMessage<{ email: string; org: string }>({
    defaultMessage: "Remove {email} from {org}? They lose access to it; their account and what they added stay, and so do any API keys they made.",
    id: "Jr2hO2",
    description: "Asked before removing a member. {email} is theirs, {org} the organisation's name",
  }),
};

const areas = [
  { name: "sales", label: m.sales, what: m.salesWhat },
  { name: "support", label: m.support, what: m.supportWhat },
] as const;

const areaLabel = (name: string) => {
  const a = areas.find((x) => x.name === name);
  return a ? intl.formatMessage(a.label) : name;
};

const languageName = (code: string) => new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;

// The languages a person can choose, each named in itself.
const languages = [locales.source, ...locales.translated].map((code) => ({ code, name: languageName(code) }));

function longDay(at: string) {
  return intl.formatDate(at, { day: "numeric", month: "short", year: "numeric" });
}

function text(form: FormData, name: string) {
  return String(form.get(name) ?? "");
}

const bold = (c: ReactNode[]) => <b className="text-foreground">{c}</b>;

function AgentsLink({ children }: { children: ReactNode }) {
  const { org } = useSession();
  return (
    <Link to="/o/$org/agents" params={{ org }} className="text-mark underline">
      {children}
    </Link>
  );
}

const agentsLink = (c: ReactNode[]) => <AgentsLink>{c}</AgentsLink>;

// openapi-fetch sends no Content-Type without a body, and the server takes a signed-in
// POST only as JSON, so one with nothing to send says so itself.
const bodiless = { "Content-Type": "application/json" };

const field = "h-10 rounded-lg border border-input bg-muted px-3 text-sm disabled:opacity-60";
const area = "rounded-lg border border-input bg-muted px-3 py-2 text-sm leading-relaxed disabled:opacity-60";
const select = "h-10 rounded-lg border border-input bg-muted px-3 text-sm";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid grid-cols-1 items-start gap-1.5 sm:grid-cols-[170px_1fr] sm:gap-4">
      <span className="pt-2.5">
        <Eyebrow>{label}</Eyebrow>
      </span>
      <span className="flex flex-col gap-1">{children}</span>
    </label>
  );
}

function Said({ children, className = "text-sm" }: { children: ReactNode; className?: string }) {
  return (
    <p className={`${className} text-[var(--ok)]`} role="status">
      {children}
    </p>
  );
}

function Tick({ name, checked, children }: { name: string; checked: boolean; children: ReactNode }) {
  return (
    <span className="flex items-center gap-2.5 pt-2 text-sm">
      <Checkbox name={name} defaultChecked={checked} />
      {children}
    </span>
  );
}

const answered = (q: { data?: unknown; error?: unknown }) => q.data !== undefined || Boolean(q.error);

export function SettingsPage() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const me = $api.useQuery("get", "/o/{org}/api/v1/me", { params: { path: { org } } });
  const name = settings.data?.name ?? "";
  // Each section holds back the ones under it until its own shape is known: Password's
  // depends on you, and What you sell's hint on the organisation's name.
  return (
    <>
      <PageHead title={say(m.settings)}>
        {(settings.data || !settings.error) && (
          <span className="text-sm text-muted-foreground">
            <FormattedMessage
              defaultMessage="For <b>{name}</b> only"
              id="1h3EMF"
              description="Beside the settings page's heading: whose settings they are. {name} is the organisation's name"
              values={{ b: bold, name: <Text value={settings.data?.name} w="8ch" /> }}
            />
          </span>
        )}
      </PageHead>
      <div className="flex max-w-3xl flex-col gap-5">
        <Failed errors={[settings.error]} />
        <Voice me={me.data} error={me.error} />
        {me.data && <Password me={me.data} />}
        {me.data && <Language me={me.data} />}
        {answered(me) && <Organisation settings={settings.data} failed={Boolean(settings.error)} />}
        {answered(me) && answered(settings) && <Rest orgName={name} me={me.data} settings={settings.data} />}
      </div>
    </>
  );
}

function Rest({ orgName, me, settings }: { orgName: string; me?: Me; settings?: Settings }) {
  const say = useIntl().formatMessage;
  const { owner } = useSession();
  return (
    <>
      {settings && <WhatYouSell orgName={orgName} />}
      {owner && <OutgoingMail orgName={orgName} />}
      {owner && <Bounces />}
      {owner && (
        <Section title={say(m.agents)}>
          <p className="text-sm">
            <FormattedMessage
              defaultMessage="{prospector}'s own agents, and keys for yours, are set up on <link>Agents</link>."
              id="MmY8lM"
              description="In the settings page's Agents section. {prospector} is the product's name; the link goes to the agents page"
              values={{ prospector, link: agentsLink }}
            />
          </p>
        </Section>
      )}
      {settings && <Sequence settings={settings} />}
      <NotToBeMailed />
      {owner && <SingleSignOn />}
      {me && <Team me={me} orgName={orgName || say(m.thisOrganisation)} />}
      {owner && <YourData />}
    </>
  );
}

// Voice is drawn before you are read, its fields empty, pulsing and disabled.
function Voice({ me, error }: { me?: Me; error?: { detail?: string } | null }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const save = $api.useMutation("patch", "/o/{org}/api/v1/me", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({ params: { path: { org } }, body: { caller_name: text(f, "caller_name"), signature: text(f, "signature") } });
  };
  return (
    <form key={me ? "me" : "loading"} onSubmit={submit} aria-busy={(!me && !error) || undefined}>
      <Section title={say(m.voice)} hint={say(m.voiceHint)}>
        {!me && error ? null : (
          <>
            <Row label={say(m.callerName)}>
              <Input
                name="caller_name"
                defaultValue={me?.caller_name}
                placeholder={me?.name}
                disabled={!me}
                className={cn(field, !me && "motion-safe:animate-pulse")}
              />
            </Row>
            <Row label={say(m.signature)}>
              <Textarea name="signature" rows={4} defaultValue={me?.signature} disabled={!me} className={cn(area, !me && "motion-safe:animate-pulse")} />
            </Row>
            <div className="flex items-center gap-3">
              <ActionButton type="submit" size="sm" disabled={!me || save.isPending}>
                {say(m.save)}
              </ActionButton>
              {save.isSuccess && <Said>{say(m.saved)}</Said>}
            </div>
          </>
        )}
        <Failed errors={[error, save.error]} />
      </Section>
    </form>
  );
}

// Language is yours in every organisation. The page is drawn in it from the start, so
// a new one takes a reload.
function Language({ me }: { me: Me }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const save = $api.useMutation("patch", "/o/{org}/api/v1/me", { onSuccess: () => window.location.reload() });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    save.mutate({ params: { path: { org } }, body: { language: text(new FormData(e.currentTarget), "language") } });
  };
  return (
    <form onSubmit={submit}>
      <Section title={say(m.language)} hint={say(m.languageHint)}>
        <Row label={say(m.languageLabel)}>
          <select name="language" defaultValue={me.language} className={cn(select, "w-full")}>
            {/* With none chosen, the page is in what the browser's languages came to. */}
            <option value="">{me.language ? say(m.browsers) : say(v.browsersIs, { language: languageName(document.documentElement.lang) })}</option>
            {languages.map((l) => (
              <option key={l.code} value={l.code} lang={l.code}>
                {l.name}
              </option>
            ))}
          </select>
          <Meta>{say(m.browsersUsed)}</Meta>
        </Row>
        <div className="flex items-center gap-3">
          <ActionButton type="submit" size="sm" disabled={save.isPending}>
            {say(m.save)}
          </ActionButton>
        </div>
        <Failed errors={[save.error]} />
      </Section>
    </form>
  );
}

function Password({ me }: { me: Me }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const change = $api.useMutation("post", "/o/{org}/api/v1/me/password");
  if (!me.has_password) {
    return (
      <Section title={say(m.signInTitle)} hint={say(m.noPassword)}>
        {null}
      </Section>
    );
  }
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    change.mutate({ params: { path: { org } }, body: { current: text(f, "current"), new: text(f, "new") } }, { onSuccess: () => form.reset() });
  };
  return (
    <form onSubmit={submit}>
      <Section title={say(m.password)} hint={say(m.passwordHint)}>
        <Row label={say(m.current)}>
          <Input name="current" type="password" required autoComplete="current-password" className={field} />
        </Row>
        <Row label={say(m.newPassword)}>
          <Input name="new" type="password" required minLength={10} autoComplete="new-password" className={field} />
        </Row>
        <div className="flex items-center gap-3">
          <ActionButton type="submit" size="sm" disabled={change.isPending}>
            {say(m.change)}
          </ActionButton>
          {change.data && <Said>{change.data.message}</Said>}
        </div>
        <Failed errors={[change.error]} />
      </Section>
    </form>
  );
}

function neverContactHint(covered: string[], total: number) {
  const hint = intl.formatMessage(m.neverHint);
  if (total === 0) return hint;
  const shown = covered.slice(0, 8);
  const more = intl.formatMessage(v.neverCovers, {
    total,
    shown: intl.formatList(shown, { type: "unit" }),
    more: Math.max(total - shown.length, 0),
  });
  return `${hint} ${more}`;
}

// Organisation is drawn before the settings come, its fields empty, pulsing and disabled;
// if they fail it stays disabled, as saving it would blank them.
function Organisation({ settings: set, failed }: { settings?: Settings; failed: boolean }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  const loading = !set && !failed;
  const box = cn(field, loading && "motion-safe:animate-pulse");
  const areaBox = cn(area, loading && "motion-safe:animate-pulse");
  const refresh = useRefresh();
  const covers = $api.useQuery("get", "/o/{org}/api/v1/never-contact/covers", { params: { path: { org }, query: { limit: 8 } } });
  const save = $api.useMutation("patch", "/o/{org}/api/v1/settings", { onSuccess: refresh });
  const intake = `${window.location.origin}/o/${org}/intake`;
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        name: text(f, "name"),
        color: text(f, "color"),
        email_after_days: Number(text(f, "email_after_days")),
        followup_after_days: Number(text(f, "followup_after_days")),
        note_template: text(f, "note_template"),
        pitch: text(f, "pitch"),
        never_contact: text(f, "never_contact"),
        look_for: text(f, "look_for"),
        agent_guidance: text(f, "agent_guidance"),
        enquiry_sites: text(f, "enquiry_sites"),
        onboarding_steps: text(f, "onboarding_steps"),
        currency: text(f, "currency"),
        postal_address: text(f, "postal_address"),
        incorporated_only: f.get("incorporated_only") !== null,
        customer_language: text(f, "customer_language"),
      },
    });
  };
  return (
    <form onSubmit={submit}>
      <Section title={say(m.organisation)} hint={owner ? say(m.shared) : say(m.ownerOnly)}>
        <fieldset key={JSON.stringify(set)} disabled={!owner || !set} aria-busy={loading || undefined} className="flex flex-col gap-4">
          <Row label={say(m.name)}>
            <Input name="name" required defaultValue={set?.name} className={box} />
          </Row>
          <Row label={say(m.colour)}>
            <Input
              name="color"
              type="color"
              defaultValue={set?.color}
              className={cn("h-10 w-20 rounded-lg border border-input bg-muted p-1", loading && "motion-safe:animate-pulse")}
            />
          </Row>
          <Row label={say(m.customerLanguage)}>
            <select name="customer_language" defaultValue={set?.customer_language} className={cn(box, "w-full")}>
              {languages.map((l) => (
                <option key={l.code} value={l.code} lang={l.code}>
                  {l.name}
                </option>
              ))}
            </select>
            <Meta>{say(m.customerLanguageHint)}</Meta>
          </Row>
          <Row label={say(m.emailAfter)}>
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <FormattedMessage
                defaultMessage="{days} days without a {linkedIn} accept"
                id="Q/VoDU"
                description="Beside the field for how many days to wait for a LinkedIn connection request to be accepted before emailing. {days} is that field; {linkedIn} is the network's name"
                values={{
                  linkedIn,
                  days: <Input name="email_after_days" type="number" min={0} max={60} defaultValue={set?.email_after_days} className={`${box} w-24`} />,
                }}
              />
            </span>
          </Row>
          <Row label={say(m.followUpAfter)}>
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <FormattedMessage
                defaultMessage="{days} days without a reply"
                id="Uc4WCj"
                description="Beside the field for how many days to wait for a reply before following up. {days} is that field"
                values={{
                  days: <Input name="followup_after_days" type="number" min={0} max={60} defaultValue={set?.followup_after_days} className={`${box} w-24`} />,
                }}
              />
            </span>
          </Row>
          <Row label={say(m.connectionNote)}>
            <Textarea name="note_template" rows={5} defaultValue={set?.note_template} className={areaBox} />
            <Meta>{say(v.noteHint, { first, company, org: orgToken, linkedIn })}</Meta>
          </Row>
          <Row label={say(m.whatYouDo)}>
            <Textarea name="pitch" rows={3} defaultValue={set?.pitch} className={areaBox} />
            <Meta>{say(v.pitchHint, { company })}</Meta>
          </Row>
          <Row label={say(m.neverContact)}>
            <Textarea name="never_contact" rows={3} placeholder={say(m.neverPlaceholder)} defaultValue={set?.never_contact} className={areaBox} />
            <Meta>{neverContactHint(covers.data?.items ?? [], covers.data?.total ?? 0)}</Meta>
          </Row>
          <Row label={say(m.lookFor)}>
            <Textarea name="look_for" rows={4} placeholder={say(m.lookForPlaceholder)} defaultValue={set?.look_for} className={areaBox} />
            <Meta>{say(v.lookForHint, { must })}</Meta>
          </Row>
          <Row label={say(m.agentGuidance)}>
            <Textarea name="agent_guidance" rows={8} placeholder={say(m.agentGuidancePlaceholder)} defaultValue={set?.agent_guidance} className={areaBox} />
            <Meta>{say(m.agentGuidanceHint)}</Meta>
          </Row>
          <Row label={say(m.enquirySites)}>
            <Textarea name="enquiry_sites" rows={2} placeholder={siteExample} defaultValue={set?.enquiry_sites} className={areaBox} />
            <Meta>{say(v.enquiryHint, { intake })}</Meta>
          </Row>
          <Row label={say(m.onboardingSteps)}>
            <Textarea
              name="onboarding_steps"
              rows={6}
              placeholder={say(v.onboardingPlaceholder, { them, us })}
              defaultValue={set?.onboarding_steps}
              className={areaBox}
            />
            <Meta>{say(v.onboardingHint, { them, us })}</Meta>
          </Row>
          <Row label={say(m.currency)}>
            <Input name="currency" defaultValue={set?.currency} maxLength={3} className={`${box} w-24`} />
            <Meta>{say(m.currencyHint)}</Meta>
          </Row>
          <Row label={say(m.postalAddress)}>
            <Textarea
              name="postal_address"
              rows={3}
              maxLength={300}
              placeholder={say(m.postalPlaceholder)}
              defaultValue={set?.postal_address}
              className={areaBox}
            />
            <Meta>{say(m.postalHint)}</Meta>
          </Row>
          <Row label={say(m.coldEmail)}>
            <Tick name="incorporated_only" checked={set?.incorporated_only ?? false}>
              {say(m.incorporatedOnly)}
            </Tick>
          </Row>
          <div className="flex items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={save.isPending}>
              {say(m.save)}
            </ActionButton>
            {save.isSuccess && <Said>{say(m.saved)}</Said>}
          </div>
        </fieldset>
        <Failed errors={[save.error, covers.error]} />
      </Section>
    </form>
  );
}

function mailHint(server: MailServer) {
  if (server.host !== "") return intl.formatMessage(m.mailOwn);
  if (server.site_mail) return intl.formatMessage(m.mailSite);
  return intl.formatMessage(m.mailNone);
}

function OutgoingMail({ orgName }: { orgName: string }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const mail = $api.useQuery("get", "/o/{org}/api/v1/mail-server", { params: { path: { org } } });
  const save = $api.useMutation("put", "/o/{org}/api/v1/mail-server", { onSuccess: refresh });
  const test = $api.useMutation("post", "/o/{org}/api/v1/mail-server/test");
  const server = mail.data;
  const loading = !server && !mail.error;
  const box = cn(field, loading && "motion-safe:animate-pulse");
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        host: text(f, "host"),
        port: Number(text(f, "port")),
        username: text(f, "username"),
        password: text(f, "password"),
        sender: text(f, "sender"),
      },
    });
  };
  return (
    <>
      <form onSubmit={submit}>
        <Section title={say(m.outgoingMail)} hint={server && mailHint(server)}>
          <fieldset key={JSON.stringify(server)} disabled={!server} aria-busy={loading || undefined} className="flex flex-col gap-4">
            <Row label={say(m.server)}>
              <span className="flex items-center gap-2">
                <Input name="host" defaultValue={server?.host} placeholder={smtpExample} className={`${box} min-w-[200px] flex-1`} />
                <Input name="port" type="number" min={1} max={65535} defaultValue={server?.port} aria-label={say(m.port)} className={`${box} w-24`} />
              </span>
              <Meta>{say(m.serverHint)}</Meta>
            </Row>
            <Row label={say(m.username)}>
              <Input name="username" defaultValue={server?.username} autoComplete="off" className={box} />
            </Row>
            <Row label={say(m.mailPassword)}>
              <Input
                name="password"
                type="password"
                autoComplete="new-password"
                placeholder={server?.has_password ? say(m.keepPassword) : ""}
                className={box}
              />
              <Meta>{say(m.passwordNeverShown)}</Meta>
            </Row>
            <Row label={say(m.from)}>
              <Input name="sender" defaultValue={server?.sender} placeholder={senderExample(orgName)} className={box} />
            </Row>
          </fieldset>
          <div className="flex items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={!server || save.isPending}>
              {say(m.save)}
            </ActionButton>
            {save.data && <Said>{save.data.host === "" ? say(m.noMailServer) : say(m.saved)}</Said>}
          </div>
          <Failed errors={[mail.error, save.error]} />
        </Section>
      </form>
      {server && (server.host !== "" || server.site_mail) && (
        <form
          className="flex flex-wrap items-center gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            test.mutate({ params: { path: { org } }, headers: bodiless });
          }}
        >
          <ActionButton quiet type="submit" size="sm" disabled={test.isPending}>
            {say(m.testEmail)}
          </ActionButton>
          {test.data && <Said>{test.data.message}</Said>}
          <Failed errors={[test.error]} />
        </form>
      )}
    </>
  );
}

// Bounces connects Postmark's webhook. Its address carries a password the server keeps
// only hashed, so it is shown once, when it is made.
function Bounces() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const mail = $api.useQuery("get", "/o/{org}/api/v1/mail-server", { params: { path: { org } } });
  const make = $api.useMutation("post", "/o/{org}/api/v1/mail-server/bounce-hook", { onSuccess: refresh });
  const drop = $api.useMutation("delete", "/o/{org}/api/v1/mail-server/bounce-hook", { onSuccess: refresh });
  const [copied, setCopied] = useState(false);
  const made = make.data && !drop.isSuccess ? make.data : undefined;
  const on = mail.data?.has_bounce_hook;
  const connect = () => {
    setCopied(false);
    drop.reset();
    make.mutate({ params: { path: { org } }, headers: bodiless });
  };
  return (
    <Section title={say(m.bounces)} hint={say(v.bouncesHint, { prospector })}>
      {!mail.data && !mail.error && (
        <Placeholders>
          <p className="text-sm">
            <Bar w="30ch" />
          </p>
        </Placeholders>
      )}
      {made && (
        <>
          <p className="text-sm">
            <FormattedMessage
              defaultMessage="Paste this address into Postmark: your server, then <b>Webhooks</b>, then <b>Add webhook</b>."
              id="uCjplR"
              description="Above the address to paste into Postmark. <b> marks the names of Postmark's own pages"
              values={{ b: bold }}
            />
          </p>
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-input bg-muted px-3 py-2">
            <code aria-label={say(m.hookAddress)} className="min-w-0 flex-1 break-all font-mono text-xs">
              {made.url}
            </code>
            <ActionButton
              quiet
              size="xs"
              onClick={() => {
                writeClipboard(made.url);
                setCopied(true);
              }}
            >
              {copied ? say(m.copiedHook) : say(m.copyHook)}
            </ActionButton>
          </div>
          <p className="text-sm">
            <FormattedMessage
              defaultMessage="Tick <b>Bounce</b> and <b>Spam Complaint</b>, and leave the other events off."
              id="HiUpGg"
              description="Which of Postmark's webhook events to turn on. <b> marks Postmark's names for them"
              values={{ b: bold }}
            />
          </p>
          <Meta>{say(m.hookShownOnce)}</Meta>
        </>
      )}
      {mail.data && !made && !on && (
        <>
          <p className="text-sm">{say(m.bouncesOff)}</p>
          <div>
            <ActionButton size="sm" disabled={make.isPending} onClick={connect}>
              {say(m.connectPostmark)}
            </ActionButton>
          </div>
          <Meta>{say(m.bouncesOffHint)}</Meta>
        </>
      )}
      {mail.data && !made && on && (
        <>
          <p className="text-sm">{say(m.bouncesOn)}</p>
          <div className="flex flex-wrap items-center gap-3">
            <ActionButton quiet size="sm" disabled={make.isPending} onClick={connect}>
              {say(m.newHookAddress)}
            </ActionButton>
            <ActionButton
              quiet
              size="sm"
              disabled={drop.isPending}
              onClick={() => window.confirm(say(m.stopBouncesAsk)) && drop.mutate({ params: { path: { org } } })}
            >
              {say(m.stopBounces)}
            </ActionButton>
          </div>
          <Meta>{say(m.newHookHint)}</Meta>
        </>
      )}
      <Failed errors={[mail.error, make.error, drop.error]} />
    </Section>
  );
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex items-center gap-2.5 rounded-lg border border-border bg-muted px-3 py-2.5">
      <span className="grid size-5 place-items-center rounded-full bg-secondary font-mono text-xs">{n}</span>
      {children}
    </li>
  );
}

function Wait({ children }: { children: ReactNode }) {
  return (
    <li className="pl-8 font-mono text-xs text-mark">
      {down}
      {children}
    </li>
  );
}

function Sequence({ settings: set }: { settings: Settings }) {
  const say = useIntl().formatMessage;
  return (
    <Section title={say(m.sequence)} hint={say(m.sequenceHint)}>
      <ol className="flex max-w-lg flex-col gap-1 text-sm">
        <Step n={1}>{say(m.stepRequest)}</Step>
        <Wait>{say(v.waitAccept, { n: set.email_after_days })}</Wait>
        <Step n={2}>{say(m.stepMessage)}</Step>
        <Wait>{say(v.waitReply, { n: set.followup_after_days })}</Wait>
        <Step n={3}>{say(m.stepFollowUp)}</Step>
        <Wait>{say(m.waitCall)}</Wait>
        <Step n={4}>{say(m.stepCall)}</Step>
      </ol>
    </Section>
  );
}

function suppressionMeta(r: Suppression) {
  const say = intl.formatMessage;
  const bounced = r.bounced_at ? say(v.bounced, { date: longDay(r.bounced_at) }) : "";
  if (r.reason === "bounced") return bounced;
  const date = longDay(r.created_at);
  const why =
    r.reason === "unsubscribed"
      ? say(v.unsubscribed, { date })
      : r.reason === "complained"
        ? say(v.complained, { date })
        : say(v.addedBy, { who: actor(r.by), date });
  return bounced ? say(v.whyAndBounced, { why, bounced }) : why;
}

function OfferingFields({ o }: { o?: Offering }) {
  const say = useIntl().formatMessage;
  return (
    <>
      <Row label={say(m.name)}>
        <Input name="name" required maxLength={60} defaultValue={o?.name} placeholder={say(m.offeringPlaceholder)} className={field} />
      </Row>
      <Row label={say(m.lookFor)}>
        <Textarea name="look_for" rows={4} defaultValue={o?.look_for} placeholder={say(m.offeringLookForPlaceholder)} className={area} />
        <Meta>{say(m.offeringLookForHint)}</Meta>
      </Row>
      <Row label={say(m.whatYouDo)}>
        <Textarea name="pitch" rows={2} defaultValue={o?.pitch} className={area} />
        <Meta>{say(m.offeringPitchHint)}</Meta>
      </Row>
      <Row label={say(m.connectionNote)}>
        <Textarea name="note_template" rows={3} defaultValue={o?.note_template} className={area} />
        <Meta>{say(v.offeringNoteHint, { first, company, org: orgToken })}</Meta>
      </Row>
    </>
  );
}

function offeringBody(form: HTMLFormElement) {
  const f = new FormData(form);
  return { name: text(f, "name"), look_for: text(f, "look_for"), pitch: text(f, "pitch"), note_template: text(f, "note_template") };
}

function WhatYouSell({ orgName }: { orgName: string }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/offerings", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/offerings", { onSuccess: refresh });
  const [adding, setAdding] = useState(false);
  const rows = list.data?.items ?? [];
  return (
    <Section title={say(m.whatYouSell)} hint={say(v.whatYouSellHint, { org: orgName })}>
      {list.data && rows.length === 0 && <p className="text-sm text-muted-foreground">{say(v.onlyOwn, { org: orgName })}</p>}
      {!list.data && !list.error && (
        <Placeholders>
          <OfferingRow />
        </Placeholders>
      )}
      {rows.map((o) => (
        <OfferingRow key={o.id} o={o} />
      ))}
      {owner &&
        (adding ? (
          <form
            className="flex flex-col gap-4 rounded-lg border border-border p-4"
            onSubmit={(e) => {
              e.preventDefault();
              add.mutate({ params: { path: { org } }, body: offeringBody(e.currentTarget) }, { onSuccess: () => setAdding(false) });
            }}
          >
            <OfferingFields />
            <div className="flex items-center gap-2">
              <ActionButton type="submit" size="sm" disabled={add.isPending}>
                {say(m.add)}
              </ActionButton>
              <ActionButton quiet type="button" size="sm" onClick={() => setAdding(false)}>
                {say(m.cancel)}
              </ActionButton>
            </div>
          </form>
        ) : (
          <div>
            <ActionButton quiet size="sm" onClick={() => setAdding(true)}>
              {say(m.addOffering)}
            </ActionButton>
          </div>
        ))}
      <Failed errors={[list.error, add.error]} />
    </Section>
  );
}

function lookingFor(fields: string[]) {
  if (fields.length === 0) return intl.formatMessage(m.nothingToLookFor);
  const shown = fields.slice(0, 4).join(dot);
  return fields.length > 4 ? shown + dot + intl.formatMessage(v.moreFields, { n: fields.length - 4 }) : shown;
}

// OfferingRow is one offering, or its outline while they load.
function OfferingRow({ o }: { o?: Offering }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const change = $api.useMutation("patch", "/o/{org}/api/v1/offerings/{id}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/offerings/{id}", { onSuccess: refresh });
  const fields = o?.looking_for.map((a) => a.field) ?? [];
  return (
    <details className="group rounded-lg border border-border">
      <summary className="flex cursor-pointer list-none flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-3">
        <span className="font-heading font-semibold">
          <Text value={o?.name} w="12ch" />
        </span>
        <span className="min-w-0 font-mono text-xs text-muted-foreground">
          <Text value={o && lookingFor(fields)} w="24ch" />
        </span>
        <span className="ml-auto font-mono text-xs text-muted-foreground">{owner ? say(m.edit) + space + chevron : chevron}</span>
      </summary>
      {o && (
        <form
          className="flex flex-col gap-4 border-t border-border px-4 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            change.mutate({ params: { path: { org, id: o.id } }, body: offeringBody(e.currentTarget) });
          }}
        >
          <fieldset key={o.updated_at} disabled={!owner} className="flex flex-col gap-4">
            <OfferingFields o={o} />
            {owner && (
              <div className="flex flex-wrap items-center gap-2">
                <ActionButton type="submit" size="sm" disabled={change.isPending}>
                  {say(m.save)}
                </ActionButton>
                {change.isSuccess && <Said>{say(m.saved)}</Said>}
                <ActionButton
                  quiet
                  type="button"
                  size="sm"
                  className="ml-auto"
                  onClick={() => window.confirm(say(v.removeOffering, { name: o.name })) && remove.mutate({ params: { path: { org, id: o.id } } })}
                >
                  {say(m.remove)}
                </ActionButton>
              </div>
            )}
            <Meta>{say(v.changedBy, { who: actor(o.updated_by) })}</Meta>
          </fieldset>
          <Failed errors={[change.error, remove.error]} />
        </form>
      )}
    </details>
  );
}

function NotToBeMailed() {
  const say = useIntl().formatMessage;
  const { org, sales, support } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/suppressions", { params: { path: { org } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/suppressions", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/suppressions/{id}", { onSuccess: refresh });
  const rows = list.data?.items ?? [];
  return (
    <Section title={say(m.notToBeMailed)} hint={say(m.notToBeMailedHint)}>
      {list.data && rows.length === 0 && <p className="text-sm text-muted-foreground">{say(m.nobody)}</p>}
      {!list.data && !list.error && (
        <Placeholders>
          {[0, 1, 2].map((i) => (
            <SuppressionRow key={`loading-${i}`} />
          ))}
        </Placeholders>
      )}
      {rows.length > 0 && (
        <div>
          {rows.map((r) => (
            <SuppressionRow key={r.id} r={r}>
              {(r.reason === "added" || r.reason === "bounced") && sales && (
                <ActionButton
                  quiet
                  size="xs"
                  onClick={() =>
                    window.confirm(
                      r.bounced_at ? say(v.mailAgainBounced, { address: r.address, date: longDay(r.bounced_at) }) : say(v.mailAgain, { address: r.address }),
                    ) && remove.mutate({ params: { path: { org, id: r.id } } })
                  }
                >
                  {say(m.takeOff)}
                </ActionButton>
              )}
            </SuppressionRow>
          ))}
        </div>
      )}
      {list.data?.next_cursor && <Meta>{say(m.newestShown)}</Meta>}
      {(sales || support) && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const form = e.currentTarget;
            add.mutate({ params: { path: { org } }, body: { address: text(new FormData(form), "address") } }, { onSuccess: () => form.reset() });
          }}
        >
          <Input name="address" type="email" required placeholder={addressExample} aria-label={say(m.addressNotToMail)} className={`${field} w-auto`} />
          <ActionButton quiet type="submit" size="sm" disabled={add.isPending}>
            {say(m.add)}
          </ActionButton>
        </form>
      )}
      <Failed errors={[list.error, add.error, remove.error]} />
    </Section>
  );
}

function SuppressionRow({ r, children }: { r?: Suppression; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-1.5 text-sm">
      <span>
        <b>
          <Text value={r?.address} w="18ch" />
        </b>
        {space}
        <span className="font-mono text-xs text-muted-foreground">
          <Text value={r && suppressionMeta(r)} w="22ch" />
        </span>
      </span>
      {children}
    </div>
  );
}

function SingleSignOn() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const sso = $api.useQuery("get", "/o/{org}/api/v1/sso", { params: { path: { org } } });
  return (
    <Section title={say(m.singleSignOn)} hint={say(v.ssoHint, { providers, oidc })}>
      {sso.data && sso.data.redirect_uri === "" && <Meta>{say(v.setURL, { variable: prospectorURL })}</Meta>}
      {sso.data && sso.data.redirect_uri !== "" && (
        <>
          <Domains />
          <Provider sso={sso.data} />
        </>
      )}
      <Failed errors={[sso.error]} />
    </Section>
  );
}

const recordCode = "select-all break-all font-mono text-xs text-foreground";

function Domains() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/sso/domains", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/sso/domains", { onSuccess: refresh });
  const verify = $api.useMutation("post", "/o/{org}/api/v1/sso/domains/{domain}/verify", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/sso/domains/{domain}", { onSuccess: refresh });
  return (
    <div className="flex flex-col gap-2">
      <Eyebrow>{say(m.yourDomains)}</Eyebrow>
      {!list.data && !list.error && (
        <Placeholders>
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-1.5 text-sm">
            <span className="flex items-center gap-2">
              <b>
                <Bar w="14ch" />
              </b>
            </span>
          </div>
        </Placeholders>
      )}
      {list.data?.items.map((d) => (
        <div key={d.domain} className="flex flex-wrap items-center justify-between gap-2 border-b border-dashed border-border py-1.5 text-sm">
          <span className="flex items-center gap-2">
            <b>{d.domain}</b>
            {d.verified_at ? <Pill tone="ok">{say(m.verified)}</Pill> : <Pill tone="warn">{say(m.notVerified)}</Pill>}
          </span>
          <span className="flex items-center gap-3">
            {!d.verified_at && (
              <ActionButton size="xs" onClick={() => verify.mutate({ params: { path: { org, domain: d.domain } }, headers: bodiless })}>
                {say(m.verify)}
              </ActionButton>
            )}
            <ActionButton
              quiet
              size="xs"
              onClick={() => window.confirm(say(v.removeDomain, { domain: d.domain })) && remove.mutate({ params: { path: { org, domain: d.domain } } })}
            >
              {say(m.remove)}
            </ActionButton>
          </span>
          {!d.verified_at && (
            <p className="w-full text-muted-foreground">
              <FormattedMessage
                defaultMessage="Add a TXT record named {record} with the value {token}, then verify."
                id="ELEaYp"
                description="Under a domain not yet verified for single sign-on. {record} is the DNS record's name and {token} its value"
                values={{ record: <code className={recordCode}>{d.record}</code>, token: <code className={recordCode}>{d.token}</code> }}
              />
            </p>
          )}
        </div>
      ))}
      <Failed errors={[list.error, verify.error, remove.error]} />
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const form = e.currentTarget;
          add.mutate({ params: { path: { org } }, body: { domain: text(new FormData(form), "domain") } }, { onSuccess: () => form.reset() });
        }}
      >
        <label className="sr-only" htmlFor="sso-domain">
          {say(m.emailDomain)}
        </label>
        <Input id="sso-domain" name="domain" required placeholder={domainExample} className={`${field} min-w-[200px] flex-1`} />
        <ActionButton quiet type="submit" size="sm" className="h-10" disabled={add.isPending}>
          {say(m.addDomain)}
        </ActionButton>
        <Failed errors={[add.error]} />
      </form>
      <Meta>{say(m.domainsHint)}</Meta>
    </div>
  );
}

function Provider({ sso }: { sso: SSO }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const save = $api.useMutation("put", "/o/{org}/api/v1/sso", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save.mutate({
      params: { path: { org } },
      body: {
        issuer: text(f, "issuer"),
        client_id: text(f, "client_id"),
        client_secret: text(f, "client_secret"),
        required: f.get("required") !== null,
        auto_join: f.get("auto_join") !== null,
      },
    });
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div key={JSON.stringify(sso)} className="flex flex-col gap-4">
        <Row label={say(m.issuer)}>
          <Input name="issuer" type="url" defaultValue={sso.issuer} placeholder={issuerExample} className={field} />
          <Meta>{say(v.issuerHint, { oidc })}</Meta>
        </Row>
        <Row label={say(m.clientID)}>
          <Input name="client_id" defaultValue={sso.client_id} autoComplete="off" className={field} />
        </Row>
        <Row label={say(m.clientSecret)}>
          <Input
            name="client_secret"
            type="password"
            autoComplete="new-password"
            placeholder={sso.has_client_secret ? say(m.keptSecret) : ""}
            className={field}
          />
          {!sso.sealing && <Meta>{say(v.sealing, { variable: secretKeys, command: secretKeyCommand })}</Meta>}
        </Row>
        <Row label={say(m.redirectURI)}>
          <code className="select-all break-all pt-2 font-mono text-xs">{sso.redirect_uri}</code>
          <Meta>{say(m.redirectHint)}</Meta>
        </Row>
        <Row label={say(m.requireIt)}>
          <Tick name="required" checked={sso.required}>
            {say(m.mustUse)}
          </Tick>
          <Meta>{say(m.requireHint)}</Meta>
        </Row>
        <Row label={say(m.newPeople)}>
          <Tick name="auto_join" checked={sso.auto_join}>
            {say(m.autoJoin)}
          </Tick>
          <Meta>{say(m.autoJoinHint)}</Meta>
        </Row>
      </div>
      <div className="flex items-center gap-3">
        <ActionButton type="submit" size="sm" disabled={save.isPending}>
          {say(m.save)}
        </ActionButton>
        {save.isSuccess && <Said>{say(m.saved)}</Said>}
      </div>
      <Failed errors={[save.error]} />
    </form>
  );
}

function memberWork(member: Member) {
  if (member.role === "owner") return intl.formatMessage(m.owner);
  if (member.areas.length === 0) return intl.formatMessage(m.readsOnly);
  return intl.formatMessage(v.memberOf, { areas: intl.formatList(member.areas.map(areaLabel), { type: "unit" }) });
}

function otherRole(role: string) {
  return role === "owner" ? "member" : "owner";
}

function roleConfirm(orgName: string, me: Me, member: Member) {
  if (member.role !== "owner") return intl.formatMessage(v.makeOwnerConfirm, { email: member.email, org: orgName });
  if (member.user_id === me.user_id) return intl.formatMessage(v.stopOwnerConfirm, { org: orgName });
  return intl.formatMessage(v.makeMemberConfirm, { email: member.email });
}

function removeConfirm(orgName: string, me: Me, member: Member) {
  if (member.user_id === me.user_id) return intl.formatMessage(v.leaveConfirm, { org: orgName });
  return intl.formatMessage(v.removeMemberConfirm, { email: member.email, org: orgName });
}

function Team({ me, orgName }: { me: Me; orgName: string }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const members = $api.useQuery("get", "/o/{org}/api/v1/members", { params: { path: { org }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/members", { onSuccess: refresh });
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    const role = text(f, "role") === "owner" ? "owner" : "member";
    add.mutate({ params: { path: { org } }, body: { email: text(f, "email"), name: text(f, "name"), role } }, { onSuccess: () => form.reset() });
  };
  return (
    <Section title={say(m.team)}>
      {!members.data && !members.error && (
        <Placeholders>
          {[0, 1, 2].map((i) => (
            <TeamMember key={`loading-${i}`} me={me} orgName={orgName} />
          ))}
        </Placeholders>
      )}
      {members.data && (
        <div>
          {members.data.items.map((member) => (
            <TeamMember key={member.user_id} member={member} me={me} orgName={orgName} />
          ))}
        </div>
      )}
      <Failed errors={[members.error]} />
      {owner && (
        <form onSubmit={submit} className="flex flex-wrap gap-2">
          <label className="sr-only" htmlFor="m-email">
            {say(m.email)}
          </label>
          <Input id="m-email" name="email" type="email" required placeholder={say(m.emailPlaceholder)} className={`${field} min-w-[200px] flex-1`} />
          <label className="sr-only" htmlFor="m-name">
            {say(m.name)}
          </label>
          <Input id="m-name" name="name" placeholder={say(m.namePlaceholder)} className={`${field} w-40`} />
          <label className="sr-only" htmlFor="m-role">
            {say(m.role)}
          </label>
          <select id="m-role" name="role" className={select}>
            <option value="member">{say(m.roleMember)}</option>
            <option value="owner">{say(m.roleOwner)}</option>
          </select>
          <ActionButton quiet type="submit" size="sm" className="h-10" disabled={add.isPending}>
            {say(m.add)}
          </ActionButton>
          {add.data && <Said className="w-full select-all font-mono text-sm">{add.data.message}</Said>}
          <Failed errors={[add.error]} />
        </form>
      )}
    </Section>
  );
}

// TeamMember is one member, or their outline while the team loads.
function TeamMember({ member, me, orgName }: { member?: Member; me: Me; orgName: string }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const self = member?.user_id === me.user_id;
  const path = { params: { path: { org, user: member?.user_id ?? 0 } } };
  // Your own role is in the page's session, so a change to it is seen by loading the page again.
  const role = $api.useMutation("patch", "/o/{org}/api/v1/members/{user}", { onSuccess: () => (self ? window.location.reload() : refresh()) });
  const work = $api.useMutation("patch", "/o/{org}/api/v1/members/{user}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/members/{user}", {
    onSuccess: () => (self ? window.location.assign("/") : refresh()),
  });
  const [picked, setPicked] = useState<string[]>(member?.areas ?? []);
  const toggle = (name: string, on: boolean) => setPicked(on ? [...picked, name] : picked.filter((a) => a !== name));
  return (
    <div className="flex flex-wrap justify-between border-b border-dashed border-border py-1.5 text-sm">
      <span>
        <b>
          <Text value={member?.name} w="10ch" />
        </b>
        {space}
        <span className="text-muted-foreground">
          <Text value={member?.email} w="18ch" />
        </span>
      </span>
      <span className="flex items-center gap-3">
        <Meta>
          <Text value={member && memberWork(member)} w="8ch" />
        </Meta>
        {owner && member && (
          <>
            <ActionButton
              quiet
              size="xs"
              onClick={() => window.confirm(roleConfirm(orgName, me, member)) && role.mutate({ ...path, body: { role: otherRole(member.role) } })}
            >
              {member.role === "owner" ? say(m.makeMember) : say(m.makeOwner)}
            </ActionButton>
            <ActionButton quiet size="xs" onClick={() => window.confirm(removeConfirm(orgName, me, member)) && remove.mutate(path)}>
              {say(m.remove)}
            </ActionButton>
          </>
        )}
      </span>
      {owner && member && member.role !== "owner" && (
        <form
          className="flex w-full flex-wrap items-center gap-3 pt-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            work.mutate({ ...path, body: { areas: areas.map((a) => a.name).filter((a) => picked.includes(a)) } });
          }}
        >
          {areas.map((a) => (
            <label key={a.name} className="flex items-center gap-1.5 text-xs text-muted-foreground" title={say(a.what)}>
              <Checkbox checked={picked.includes(a.name)} onCheckedChange={(on) => toggle(a.name, on === true)} />
              {say(a.label)}
            </label>
          ))}
          <ActionButton quiet type="submit" size="xs" disabled={work.isPending}>
            {say(m.save)}
          </ActionButton>
          {work.isSuccess && <Said className="text-xs">{say(m.saved)}</Said>}
        </form>
      )}
      <Failed errors={[role.error, work.error, remove.error]} />
    </div>
  );
}

function YourData() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const exported = $api.useMutation("post", "/o/{org}/api/v1/export", {
    onSuccess: (data) => {
      const a = document.createElement("a");
      a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
      a.download = `${org}.json`;
      a.click();
      URL.revokeObjectURL(a.href);
    },
  });
  return (
    <Section title={say(m.yourData)}>
      <p className="text-sm text-muted-foreground">{say(m.yourDataHint)}</p>
      <div>
        <ActionButton quiet size="sm" disabled={exported.isPending} onClick={() => exported.mutate({ params: { path: { org } }, headers: bodiless })}>
          {say(m.download)}
        </ActionButton>
      </div>
      <Failed errors={[exported.error]} />
    </Section>
  );
}
