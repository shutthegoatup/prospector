// Bundles src/app.ts into the committed assets/js/app.js, which the server embeds:
// running Prospector needs no Node, only changing the script does. The agreement field
// editor is a bundle of its own, so pdf.js loads on its page alone.
import { copyFile } from "node:fs/promises";
import { build } from "esbuild";

const common = { bundle: true, target: "es2022", legalComments: "none", logLevel: "info" };

await build({
  ...common,
  entryPoints: ["src/app.ts"],
  outfile: "../assets/js/app.js",
  format: "iife",
  banner: { js: "// Built from web/src/app.ts by `npm run build` in web/. Don't edit by hand." },
});

await build({
  ...common,
  entryPoints: ["src/prepare.ts"],
  outfile: "../assets/js/prepare.js",
  format: "esm",
  minify: true,
  banner: { js: "// Built from web/src/prepare.ts and pdf.js (Apache-2.0) by `npm run build` in web/. Don't edit by hand." },
});

await copyFile("node_modules/pdfjs-dist/build/pdf.worker.min.mjs", "../assets/js/pdf.worker.min.mjs");
await copyFile("node_modules/pdfjs-dist/LICENSE", "../assets/js/pdf.js.LICENSE.txt");
