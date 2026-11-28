import { StrictMode } from "react";
import { renderToString } from "react-dom/server";
import { RawIntlProvider } from "react-intl";
import { intl, speak, type Messages } from "@/i18n";
import locales from "../../locales.json";
import { ApiReference, type Doc } from "./ApiReference";
import doc from "./apidoc.json";
import { head, Landing } from "./Landing";

export { pricing } from "./Landing";

// What prerender.mjs writes into the built pages, rendered when the app is built: the
// front page once in each language, the reference once, in English like the spec.
export function landing(locale: string, messages: Messages) {
  speak(locale, messages);
  return {
    title: intl.formatMessage(head.title),
    description: intl.formatMessage(head.description),
    html: renderToString(
      <StrictMode>
        <RawIntlProvider value={intl}>
          <Landing />
        </RawIntlProvider>
      </StrictMode>,
    ),
  };
}

export function apiReference(): string {
  speak(locales.source, {});
  return renderToString(
    <RawIntlProvider value={intl}>
      <ApiReference doc={doc as Doc} />
    </RawIntlProvider>,
  );
}
