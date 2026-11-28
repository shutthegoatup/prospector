// Writes the public site's pages, rendered by React when the app is built, into the
// embedded assets/app: the front page in each language (site.<locale>.html) and the
// /api reference (api.html), which is in English like the spec it is built from.
// Search engines, link previews and readers without script get the whole page; the
// front page's script only adds its enquiry form.
import { readFile, writeFile } from "node:fs/promises";
import { apiReference, landing, pricing } from "./site-build/render.js";

const head = (title, description, styles) =>
  [
    `<meta charset="utf-8" />`,
    `<meta name="viewport" content="width=device-width, initial-scale=1" />`,
    `<meta name="color-scheme" content="light" />`,
    `<meta name="theme-color" content="#ffc233" />`,
    `<title>${title}</title>`,
    `<meta name="description" content="${description}" />`,
    `<link rel="icon" type="image/svg+xml" href="/assets/img/mark.svg" />`,
    ...["fonts", ...styles].map((css) => `<link rel="stylesheet" href="/assets/css/${css}.css" />`),
  ].join("\n    ");

const page = (lang, headHTML, bodyClass, root) => `<!DOCTYPE html>
<html lang="${lang}">
  <head>
    ${headHTML}
  </head>
  <body class="${bodyClass}">
    ${root}
  </body>
</html>
`;

const site = "overflow-x-clip bg-background font-sans text-[17px] leading-[1.6] text-foreground antialiased";

const locales = JSON.parse(await readFile("locales.json", "utf8"));
const attr = (text) => text.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;");

for (const locale of [locales.source, ...locales.translated, ...locales.pseudo]) {
  const messages = locale === locales.source ? {} : JSON.parse(await readFile(`src/lang/site/${locale}.json`, "utf8"));
  const front = landing(locale, messages);
  // The prices are hosted terms, and stay English: only the English description says them.
  await writeFile(
    `../assets/app/site.${locale}.html`,
    page(
      locale,
      head(attr(front.title), attr(locale === locales.source ? `${front.description} ${pricing}` : front.description), ["output"]) +
        `\n    <script type="module" src="/assets/app/site.js"></script>`,
      site,
      `<div id="site">${front.html}</div>`,
    ),
  );
}

await writeFile(
  "../assets/app/api.html",
  page(
    "en",
    head(
      "Prospector API · The contract",
      "Everything Prospector does, as an HTTP API for people and robots alike: agents, runs and more, with the OpenAPI spec to build against.",
      ["output"],
    ),
    site,
    apiReference(),
  ),
);
console.log("wrote assets/app/site.<locale>.html and assets/app/api.html");
