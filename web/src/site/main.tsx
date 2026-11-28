import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RawIntlProvider } from "react-intl";
import { intl, speak, type Messages } from "@/i18n";
import { EnquiryForm } from "./Landing";

// Only the site's share of each catalogue (i18n.mjs), each its own chunk.
const catalogs = import.meta.glob<Messages>("../lang/site/*.json", { import: "default" });

// The front page is built as HTML; this script only brings its enquiry form, and the
// server loads it only where it has somewhere for the form to send. The form shows only
// on the origin the intake route answers: reached by another name, it would be refused.
const site = document.getElementById("site");
const mount = document.getElementById("enquiry");
if (site && mount) {
  let intake = "";
  try {
    const got = JSON.parse(site.dataset.page ?? "null");
    if (got && typeof got.intake === "string" && got.origin === window.location.origin) intake = got.intake;
  } catch {
    intake = "";
  }
  if (intake) {
    // The page was built in its language (prerender.mjs); the form speaks it too, or
    // English if its messages can't be fetched.
    const lang = document.documentElement.lang;
    const messages = await catalogs[`../lang/site/${lang}.json`]?.().catch(() => undefined);
    if (messages) speak(lang, messages);
    createRoot(mount).render(
      <StrictMode>
        <RawIntlProvider value={intl}>
          <EnquiryForm intake={intake} nav={document.getElementById("site-links")} plans={[...document.querySelectorAll<HTMLElement>("[data-plan-cta]")]} />
        </RawIntlProvider>
      </StrictMode>,
    );
  }
}
