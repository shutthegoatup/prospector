// Bundles src/app.ts into the committed assets/js/app.js, which the server embeds:
// running Prospector needs no Node, only changing the script does. pdf.js's worker is
// copied beside it, where the app's agreement field editor loads it from.
//
// The front page (src/front) is prerendered into assets/front/index.html, so a visitor
// and a crawler get the whole page as HTML, and front.js hydrates it.
import { copyFile, mkdir, rm, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
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

await mkdir("../assets/front", { recursive: true });
await build({
  bundle: true,
  minify: true,
  target: "es2022",
  legalComments: "none",
  logLevel: "info",
  jsx: "automatic",
  define: { "process.env.NODE_ENV": '"production"' },
  entryPoints: ["src/front/main.tsx"],
  outfile: "../assets/front/front.js",
  format: "iife",
  banner: { js: "// Built from web/src/front by `npm run build` in web/. Don't edit by hand." },
});

const renderer = "node_modules/.front-render.mjs";
await build({
  bundle: true,
  platform: "node",
  format: "esm",
  jsx: "automatic",
  packages: "external",
  logLevel: "warning",
  entryPoints: ["src/front/render.tsx"],
  outfile: renderer,
});
process.env.NODE_ENV = "production";
const { frontDocument } = await import(pathToFileURL(renderer).href);
await writeFile("../assets/front/index.html", frontDocument());
await rm(renderer);
