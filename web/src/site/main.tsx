import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { EnquiryForm } from "./Landing";

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
    createRoot(mount).render(
      <StrictMode>
        <EnquiryForm intake={intake} nav={document.getElementById("site-links")} plans={[...document.querySelectorAll<HTMLElement>("[data-plan-cta]")]} />
      </StrictMode>,
    );
  }
}
