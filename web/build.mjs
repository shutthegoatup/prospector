// Bundles src/app.ts into the committed assets/js/app.js, which the server embeds:
// running Prospector needs no Node, only changing the script does. pdf.js's worker is
// copied beside it, where the app's agreement field editor loads it from.
import { copyFile } from "node:fs/promises";
import { build } from "esbuild";

await build({
  bundle: true,
  target: "es2022",
  legalComments: "none",
  logLevel: "info",
  entryPoints: ["src/app.ts"],
  outfile: "../assets/js/app.js",
  format: "iife",
  banner: { js: "// Built from web/src/app.ts by `npm run build` in web/. Don't edit by hand." },
});

await copyFile("node_modules/pdfjs-dist/build/pdf.worker.min.mjs", "../assets/js/pdf.worker.min.mjs");
await copyFile("node_modules/pdfjs-dist/LICENSE", "../assets/js/pdf.js.LICENSE.txt");
