// Gathers every message the app defines into lang/<source>.json (committed, so CI sees
// a change to it), checks each translation in lang/ against it, and compiles them all,
// pseudo-locales included, into src/lang/, which the app loads one at a time.
import { execFileSync } from "node:child_process";
import { mkdir, readFile, rm } from "node:fs/promises";

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
  "--out-file",
  source,
  "--throws",
);

// A message with no id is given a hash by extract that the running app never sees.
const unnamed = Object.keys(JSON.parse(await readFile(source, "utf8"))).filter((id) => !/^[a-z][\w-]*(\.[\w-]+){2,}$/i.test(id));
if (unnamed.length) throw new Error(`these messages need an id like "page.part.what": ${unnamed.join(", ")}`);

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
