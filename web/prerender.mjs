// Writes the public site's pages, rendered by React when the app is built, into the
// embedded assets/app: the front page (site.html) and the /api reference (api.html).
// Search engines, link previews and readers without script get the whole page; the
// front page's script only adds its enquiry form.
import { writeFile } from "node:fs/promises";
import { apiReference, landing } from "./site-build/render.js";

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

const page = (headHTML, bodyClass, root) => `<!DOCTYPE html>
<html lang="en">
  <head>
    ${headHTML}
  </head>
  <body class="${bodyClass}">
    ${root}
  </body>
</html>
`;

await writeFile(
  "../assets/app/site.html",
  page(
    head(
      "Prospector · A few hundred accounts, one queue a day",
      "A prospecting and customer workbench for small teams selling to named accounts: research, deals, subscriptions and onboarding on one company record. Agents research and draft; you decide what they may send.",
      ["landing"],
    ) + `\n    <script type="module" src="/assets/app/site.js"></script>`,
    "lp",
    `<div id="site">${landing()}</div>`,
  ),
);

await writeFile(
  "../assets/app/api.html",
  page(
    head(
      "Prospector API · The contract",
      "Everything Prospector does, as an HTTP API for people and robots alike: agents, runs and more, with the OpenAPI spec to build against.",
      ["landing", "api"],
    ),
    "lp api",
    apiReference(),
  ),
);
console.log("wrote assets/app/site.html and assets/app/api.html");
