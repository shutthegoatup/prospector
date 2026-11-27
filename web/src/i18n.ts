import { createIntl, createIntlCache, type IntlShape, type MessageFormatElement } from "react-intl";
import locales from "../locales.json";

// The app is written in locales.source; a message's defaultMessage is that text, and
// a catalogue (web/lang/<locale>.json, compiled into src/lang/) holds the others.
export type Messages = Record<string, MessageFormatElement[]>;

const cache = createIntlCache();

function make(locale: string, messages: Messages): IntlShape {
  return createIntl(
    {
      locale,
      defaultLocale: locales.source,
      messages,
      onError: (err) => {
        // The source language has no catalogue: its text is each message's default.
        if (err.code !== "MISSING_TRANSLATION") console.error(err);
      },
    },
    cache,
  );
}

// intl is the one the whole app speaks through, set once before the first page is
// drawn, so the helpers outside React (lib/format.ts) speak the same language.
export let intl = make(locales.source, {});

export function speak(locale: string, messages: Messages) {
  intl = make(locale, messages);
}
