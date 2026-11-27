import { strict as assert } from "node:assert";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { test } from "node:test";

// oxlint's react/jsx-no-literals catches English between tags; this catches it in the
// attributes a screen reader or a tooltip reads out. Both spare the files listed in
// .oxlintrc.json, which are still to be translated: the list only shrinks.
const lint = JSON.parse(readFileSync(new URL("../.oxlintrc.json", import.meta.url), "utf8"));
const untranslated: string[] = lint.overrides.find((o: { rules: Record<string, string> }) => o.rules["react/jsx-no-literals"] === "off").files;

function sources(dir: string): string[] {
  return readdirSync(web + dir, { withFileTypes: true }).flatMap((e) => {
    const path = `${dir}/${e.name}`;
    if (e.isDirectory()) return path === "src/components/ui" ? [] : sources(path);
    return path.endsWith(".tsx") ? [path] : [];
  });
}

const web = new URL("..", import.meta.url).pathname;

const spoken = /\b(aria-label|aria-description|aria-placeholder|aria-roledescription|aria-valuetext|title|placeholder|alt)="[^"]*\p{L}/u;

test("what is read out in a translated file goes through intl", () => {
  const found = sources("src")
    .filter((f) => !untranslated.includes(f))
    .flatMap((f) =>
      readFileSync(web + f, "utf8")
        .split("\n")
        .flatMap((line, i) => (spoken.test(line) ? [`${f}:${i + 1}: ${line.trim()}`] : [])),
    );
  assert.deepEqual(found, []);
});

test("every file still to translate is there", () => {
  assert.deepEqual(
    untranslated.filter((f) => !existsSync(web + f)),
    [],
  );
});
