import { useState, type FormEvent } from "react";
import { outcomeText, send } from "./enquiry";

export function EnquiryForm({ intake }: { intake: string }) {
  const [status, setStatus] = useState("");
  const [sending, setSending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const field = (name: string) => String(new FormData(form).get(name) ?? "").trim();
    const body = JSON.stringify({
      kind: field("kind"),
      name: field("name"),
      email: field("email"),
      company: field("company"),
      message: field("message"),
      trap: field("ref_note"),
      page: location.origin + location.pathname,
    });
    setSending(true);
    setStatus("Sending…");
    const outcome = await send(intake, body);
    setSending(false);
    if (outcome === "sent") form.reset();
    setStatus(outcomeText[outcome]);
  }

  return (
    <section className="lp-contact" id="contact">
      <div className="lp-wrap lp-contact-grid">
        <div>
          <p className="lp-label">Contact</p>
          <h2>Ask us anything.</h2>
          <p className="lp-lede">Hosting, a question, a demo, or help getting it running. Someone will write back to the address you give.</p>
        </div>
        <form className="lp-form" onSubmit={submit}>
          <label>
            Your name
            <input name="name" autoComplete="name" maxLength={200} />
          </label>
          <label>
            Email
            <input name="email" type="email" autoComplete="email" maxLength={200} required />
          </label>
          <label>
            Company
            <input name="company" autoComplete="organization" maxLength={200} />
          </label>
          <label>
            What it's about
            <select name="kind" defaultValue="sales">
              <option value="signup">Hosted Prospector</option>
              <option value="sales">A question</option>
              <option value="demo">See it running</option>
              <option value="support">Help running it</option>
              <option value="feature">Something it should do</option>
            </select>
          </label>
          <label className="is-wide">
            Message
            <textarea name="message" rows={5} maxLength={5000} required></textarea>
          </label>
          <label className="lp-trap" aria-hidden="true">
            Leave this empty
            <input name="ref_note" tabIndex={-1} autoComplete="off" />
          </label>
          <div className="lp-form-foot is-wide">
            <button className="lp-btn is-primary" type="submit" disabled={sending}>
              Send
            </button>
            <p className="lp-form-status" role="status">
              {status}
            </p>
          </div>
        </form>
      </div>
    </section>
  );
}
