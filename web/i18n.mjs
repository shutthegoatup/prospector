// Gathers every message the app defines into lang/<source>.json (committed, so CI sees
// a change to it), checks each translation in lang/ against it, and compiles them all,
// pseudo-locales included, into src/lang/, which the app loads one at a time, and
// the public site's share of each into src/lang/site/.
import { execFileSync } from "node:child_process";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";

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

if (locales.translated.length) {
  formatjs(
    "verify",
    source,
    ...locales.translated.map((l) => `lang/${l}.json`),
    "--source-locale",
    locales.source,
    "--missing-keys",
    "--extra-keys",
    "--structural-equality",
  );
}

await rm("src/lang", { recursive: true, force: true });
await mkdir("src/lang");
for (const l of locales.translated) formatjs("compile", `lang/${l}.json`, "--ast", "--out-file", `src/lang/${l}.json`);
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
