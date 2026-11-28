// Gathers every message the app defines into lang/<source>.json (committed, so CI sees
// a change to it), checks each translation in lang/ against it (below), and compiles them all,
// pseudo-locales included, into src/lang/, which the app loads one at a time, and
// the public site's share of each into src/lang/site/.
import { execFileSync } from "node:child_process";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { parse, TYPE } from "@formatjs/icu-messageformat-parser";

const locales = JSON.parse(await readFile("locales.json", "utf8"));
const formatjs = (...args) => execFileSync("node_modules/.bin/formatjs", args, { stdio: "inherit" });
const source = `lang/${locales.source}.json`;

await mkdir("lang", { recursive: true });
formatjs(
  "extract",
  "src/**/*.{ts,tsx}",
  "--ignore",
  "src/**/*.test.ts",
  "--ignore",
  "src/**/*.d.ts",
  "--ignore",
  "src/components/ui/**",
  "--id-interpolation-pattern",
  "[sha512:contenthash:base64:6]",
  "--out-file",
  source,
  "--throws",
);

// A translation is the simple format, { id: text }. One whose message is gone is dropped,
// so changing the English asks for it again; a message not yet translated reads in
// English. One that takes other values, tags or choices than the source fails the build.
const english = Object.fromEntries(Object.entries(JSON.parse(await readFile(source, "utf8"))).map(([id, m]) => [id, m.defaultMessage]));
const shape = (message) => {
  const out = new Set();
  const walk = (elements) => {
    for (const e of elements) {
      if (e.type === TYPE.tag) {
        out.add(`<${e.value}>`);
        walk(e.children);
      } else if ("value" in e && e.type !== TYPE.literal && e.type !== TYPE.pound) out.add(`{${e.value}, ${TYPE[e.type]}}`);
      if (e.type === TYPE.select) for (const key of Object.keys(e.options)) out.add(`{${e.value}, select, ${key}}`);
      if (e.options) for (const o of Object.values(e.options)) walk(o.value);
    }
  };
  walk(parse(message));
  return [...out].sort().join(" ");
};
let wrong = 0;
for (const l of locales.translated) {
  const file = `lang/${l}.json`;
  const said = JSON.parse(await readFile(file, "utf8"));
  const kept = Object.fromEntries(Object.entries(said).filter(([id]) => id in english).sort(([a], [b]) => (a < b ? -1 : 1)));
  for (const [id, text] of Object.entries(kept)) {
    let got;
    try {
      got = shape(text);
    } catch (err) {
      got = `unparsable: ${err.message}`;
    }
    if (got !== shape(english[id])) {
      wrong++;
      console.error(`${file} ${id}: ${JSON.stringify(text)} has ${got}, not ${shape(english[id])}`);
    }
  }
  if (Object.keys(kept).length !== Object.keys(said).length) await writeFile(file, `${JSON.stringify(kept, null, 2)}\n`);
  const missing = Object.keys(english).length - Object.keys(kept).length;
  if (missing) console.log(`${file}: ${missing} message(s) not yet translated, shown in English`);
}
if (wrong) throw new Error(`${wrong} translation(s) don't fit their message`);

await rm("src/lang", { recursive: true, force: true });
await mkdir("src/lang");
for (const l of locales.translated) formatjs("compile", `lang/${l}.json`, "--format", "simple", "--ast", "--out-file", `src/lang/${l}.json`);
for (const l of locales.pseudo) formatjs("compile", source, "--ast", "--pseudo-locale", l, "--out-file", `src/lang/${l}.json`);

// The public site loads only its own messages: the front page's form would otherwise
// fetch the whole app's catalogue.
const siteIds = `${tmpdir()}/prospector-site-${process.pid}.json`;
formatjs("extract", "src/site/**/*.tsx", "src/components/app/site.tsx", "--id-interpolation-pattern", "[sha512:contenthash:base64:6]", "--out-file", siteIds);
const ids = Object.keys(JSON.parse(await readFile(siteIds, "utf8")));
await rm(siteIds);
await mkdir("src/lang/site");
for (const l of [...locales.translated, ...locales.pseudo]) {
  const all = JSON.parse(await readFile(`src/lang/${l}.json`, "utf8"));
  await writeFile(`src/lang/site/${l}.json`, JSON.stringify(Object.fromEntries(ids.filter((id) => id in all).map((id) => [id, all[id]]))));
}
