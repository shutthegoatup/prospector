const section = document.getElementById("contact");
const form = section?.querySelector("form");

// The server may be reached at an address its organisation hasn't listed, where every
// enquiry would be refused: the form shows only where it can send.
if (form && form.dataset.origin === location.origin) {
  const status = form.querySelector(".lp-form-status");
  const button = form.querySelector("button");
  section.hidden = false;
  for (const link of document.querySelectorAll("[data-needs-script]")) link.hidden = false;

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const field = (name) => form.elements[name].value.trim();
    const body = JSON.stringify({
      kind: field("kind"),
      name: field("name"),
      email: field("email"),
      company: field("company"),
      message: field("message"),
      trap: field("ref_note"),
      page: location.origin + location.pathname,
    });
    button.disabled = true;
    status.textContent = "Sending…";
    const outcome = await send(form.dataset.intake, body);
    button.disabled = false;
    if (outcome === "sent") {
      form.reset();
      status.textContent = "Sent. Someone will write back to you.";
    } else if (outcome === "invalid") {
      status.textContent = "That couldn't be taken. Check the email address and try again.";
    } else if (outcome === "busy") {
      status.textContent = "Too many messages just now. Your message is still here: try again in an hour.";
    } else if (outcome === "refused") {
      status.textContent = "This form can't send from here at the moment.";
    } else {
      status.textContent = "It didn't send. Your message is still here: try again in a minute.";
    }
  });
}

// The server keeps a repeated enquiry once, so a dropped answer is safe to send again.
// An answer that says no would say no again.
async function send(to, body) {
  for (let attempt = 0; attempt < 3; attempt++) {
    if (attempt) await new Promise((done) => setTimeout(done, attempt * 1500));
    try {
      const res = await fetch(to, { method: "POST", headers: { "content-type": "application/json" }, body });
      if (res.ok) return "sent";
      if (res.status === 400) return "invalid";
      if (res.status === 429) return "busy";
      if (res.status < 500) return "refused";
    } catch {
      // offline or cut off: try again
    }
  }
  return "failed";
}
