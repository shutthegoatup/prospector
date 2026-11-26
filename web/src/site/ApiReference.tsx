import { Fragment } from "react";

// The shape of apidoc.json, which the server's test writes from api/openapi.yaml.
export type DocSpan = { text: string; code?: boolean };
export type DocType = { name: string; link?: string };
export type DocField = { name: string; type: DocType; about: DocSpan[]; required?: boolean; write?: boolean };
export type DocResponse = { code: string; bad?: boolean; about: string; type?: DocType };
export type DocOp = {
  anchor: string;
  method: string;
  tone: string;
  path: string;
  summary: string;
  about: DocSpan[];
  scope?: string;
  owner_only?: boolean;
  params: DocField[];
  body?: { of?: DocType; fields: DocField[] };
  responses: DocResponse[];
  curl: string;
};
export type DocSchema = { anchor: string; name: string; about: DocSpan[]; enum: string[]; fields: DocField[] };
export type Doc = {
  title: string;
  version: string;
  intro: DocSpan[][];
  scopes: { name: string; what: string; owner?: boolean }[];
  groups: { name: string; about: DocSpan[]; ops: DocOp[] }[];
  schemas: DocSchema[];
};

const repoURL = "https://github.com/shutthegoatup/prospector";

function Spans({ of }: { of: DocSpan[] }) {
  return (
    <>
      {of.map((sp, i) => (
        <Fragment key={i}>{sp.code ? <code>{sp.text}</code> : sp.text}</Fragment>
      ))}
    </>
  );
}

function TypeName({ t }: { t: DocType }) {
  return t.link ? (
    <a className="api-type" href={t.link}>
      {t.name}
    </a>
  ) : (
    <span className="api-type">{t.name}</span>
  );
}

function Fields({ of }: { of: DocField[] }) {
  return (
    <dl className="api-fields">
      {of.map((f) => (
        <div className="api-field" key={f.name}>
          <dt>
            <code className="api-name">{f.name}</code>
            <TypeName t={f.type} />
            {f.required && <span className="api-flag is-req">required</span>}
            {f.write && <span className="api-flag">write-only</span>}
          </dt>
          {f.about.length > 0 && (
            <dd>
              <Spans of={f.about} />
            </dd>
          )}
        </div>
      ))}
    </dl>
  );
}

function Brand() {
  return (
    <a className="lp-brand" href="/">
      <img src="/assets/img/mark.svg" width={30} height={30} alt="" />
      <span>Prospector.</span>
    </a>
  );
}

const problemExample = `HTTP/1.1 403 Forbidden
Content-Type: application/problem+json

{"type":"about:blank","title":"Forbidden","status":403,"detail":"this key lacks the agents:configure scope"}`;

// ApiReference is the page at /api: the API's contract, as people read it. It is drawn
// when the site is built and runs no script.
export function ApiReference({ doc }: { doc: Doc }) {
  return (
    <>
      <header className="api-top">
        <div className="lp-wrap">
          <nav className="lp-nav" aria-label="Site">
            <Brand />
            <div className="lp-links">
              <a href="/">Product</a>
              <a href="#auth">Authentication</a>
              <a href="#schemas">Schemas</a>
            </div>
            <a className="lp-btn is-small" href="/api/openapi.json">
              openapi.json
            </a>
          </nav>
          <div className="api-hero">
            <p className="lp-label">{`${doc.title} API · v${doc.version} · OpenAPI 3`}</p>
            <h1>The contract.</h1>
            {doc.intro.map((p, i) => (
              <p className="api-lede" key={i}>
                <Spans of={p} />
              </p>
            ))}
          </div>
        </div>
      </header>
      <div className="lp-wrap api-body">
        <aside className="api-side" aria-label="Contents">
          <p className="lp-label">Start</p>
          <a href="#auth">Authentication</a>
          <a href="#errors">Errors</a>
          {doc.groups.map((g) => (
            <Fragment key={g.name}>
              <p className="lp-label">{g.name}</p>
              {g.ops.map((o) => (
                <a key={o.anchor} href={`#${o.anchor}`}>
                  <span className={`api-m ${o.tone}`}>{o.method}</span>
                  {o.summary}
                </a>
              ))}
            </Fragment>
          ))}
          <p className="lp-label">Schemas</p>
          {doc.schemas.map((s) => (
            <a key={s.anchor} href={`#${s.anchor}`}>
              {s.name}
            </a>
          ))}
        </aside>
        <main className="api-main">
          <section id="auth" className="api-section">
            <h2>Authentication</h2>
            <p>
              Send an API key made on your organisation's Agents page as a bearer token. Every path starts with your organisation's slug, and a key only opens its
              own organisation.
            </p>
            <pre className="api-code">
              <code>Authorization: Bearer pk_…</code>
            </pre>
            <p>Prospector's own pages use the same API with the signed-in session. A member can read and run; changing settings needs an owner.</p>
            <table className="api-table">
              <thead>
                <tr>
                  <th>Scope</th>
                  <th>Lets a key</th>
                </tr>
              </thead>
              <tbody>
                {doc.scopes.map((s) => (
                  <tr key={s.name}>
                    <td>
                      <code>{s.name}</code>
                      {s.owner && <span className="api-flag">owner-level</span>}
                    </td>
                    <td>{s.what}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
          <section id="errors" className="api-section">
            <h2>Errors</h2>
            <p>
              A failure answers with its status and an <a href="https://www.rfc-editor.org/rfc/rfc9457">RFC 9457</a> problem, whose <code>detail</code> says what to
              change.
            </p>
            <pre className="api-code">
              <code>{problemExample}</code>
            </pre>
          </section>
          {doc.groups.map((g) => (
            <section className="api-section" key={g.name}>
              <h2 className="api-group">{g.name}</h2>
              {g.about.length > 0 && (
                <p className="api-soft">
                  <Spans of={g.about} />
                </p>
              )}
              {g.ops.map((o) => (
                <article id={o.anchor} className="api-op" key={o.anchor}>
                  <header className="api-op-head">
                    <span className={`api-m is-big ${o.tone}`}>{o.method}</span>
                    <code className="api-path">{o.path}</code>
                  </header>
                  <h3>{o.summary}</h3>
                  {o.about.length > 0 && (
                    <p>
                      <Spans of={o.about} />
                    </p>
                  )}
                  <p className="api-scope">
                    {o.scope && (
                      <>
                        Key scope <code>{o.scope}</code>
                      </>
                    )}
                    {o.owner_only && <span className="api-flag">owner, when signed in</span>}
                  </p>
                  <div className="api-op-grid">
                    <div>
                      {o.params.length > 0 && (
                        <>
                          <h4>Path</h4>
                          <Fields of={o.params} />
                        </>
                      )}
                      {o.body && (
                        <>
                          <h4>
                            Body
                            {o.body.of && <TypeName t={o.body.of} />}
                          </h4>
                          <Fields of={o.body.fields} />
                        </>
                      )}
                      <h4>Responses</h4>
                      <dl className="api-fields">
                        {o.responses.map((r) => (
                          <div className="api-field" key={r.code}>
                            <dt>
                              <span className={r.bad ? "api-code-tag is-bad" : "api-code-tag"}>{r.code}</span>
                              {r.type && <TypeName t={r.type} />}
                            </dt>
                            <dd>{r.about}</dd>
                          </div>
                        ))}
                      </dl>
                    </div>
                    <pre className="api-code is-dark">
                      <code>{o.curl}</code>
                    </pre>
                  </div>
                </article>
              ))}
            </section>
          ))}
          <section id="schemas" className="api-section">
            <h2>Schemas</h2>
            {doc.schemas.map((s) => (
              <article id={s.anchor} className="api-schema" key={s.anchor}>
                <h3>
                  <code>{s.name}</code>
                </h3>
                {s.about.length > 0 && (
                  <p>
                    <Spans of={s.about} />
                  </p>
                )}
                {s.enum.length > 0 && (
                  <p className="api-enum">
                    {s.enum.map((e) => (
                      <code key={e}>{e}</code>
                    ))}
                  </p>
                )}
                {s.fields.length > 0 && <Fields of={s.fields} />}
              </article>
            ))}
          </section>
        </main>
      </div>
      <footer className="lp-foot">
        <div className="lp-wrap">
          <Brand />
          <span>
            The spec is <a href="/api/openapi.json">openapi.json</a>; generate a client from it in any language.
          </span>
          <nav aria-label="Footer">
            <a href={repoURL}>GitHub</a>
            <a href="/">Product</a>
            <a href="/login">Sign in</a>
          </nav>
        </div>
      </footer>
    </>
  );
}
