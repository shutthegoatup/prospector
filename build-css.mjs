// Builds assets/css/output.css with an existing Tailwind v4 install, for machines without
// the standalone tailwindcss CLI. Usage: node build-css.mjs <node_modules dir>
// (a node_modules folder with tailwindcss v4 and @tailwindcss/node, e.g. after `npm i tailwindcss @tailwindcss/node`).
import { readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
if (!process.argv[2]) throw new Error("usage: node build-css.mjs <node_modules dir>");
const modules = path.resolve(process.argv[2]);
const require = createRequire(path.join(modules, "noop.js"));
const { compile, optimize } = require("@tailwindcss/node");
const { Scanner } = require("@tailwindcss/oxide");

const input = path.join(here, "assets/css/globals.css");
const compiler = await compile(readFileSync(input, "utf8"), {
  base: path.dirname(input),
  onDependency: () => {},
  customCssResolver: async (id) => (id === "tailwindcss" ? require.resolve("tailwindcss/index.css") : undefined),
});
const sources = compiler.root === "none" ? compiler.sources : [{ base: here, pattern: "**/*", negated: false }, ...compiler.sources];
const css = compiler.build(new Scanner({ sources }).scan());
writeFileSync(path.join(here, "assets/css/output.css"), optimize(css, { minify: true }).code);
console.log("wrote assets/css/output.css");
