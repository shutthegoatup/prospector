import { StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import { Landing, type Intake } from "./Landing";

// The front page is built as HTML; this script only brings its enquiry form, so the
// server loads it only where it has somewhere for the form to send.
const mount = document.getElementById("site");
if (mount) {
  let intake: Intake | undefined;
  try {
    const got = JSON.parse(mount.dataset.page ?? "null");
    if (got && typeof got.intake === "string" && typeof got.origin === "string") intake = { intake: got.intake, origin: got.origin };
  } catch {
    intake = undefined;
  }
  hydrateRoot(
    mount,
    <StrictMode>
      <Landing intake={intake} />
    </StrictMode>,
  );
}
