// Bundles src/app.ts into the committed assets/js/app.js, which the server embeds:
// running Prospector needs no Node, only changing the script does.
import { build } from "esbuild";

await build({
  entryPoints: ["src/app.ts"],
  outfile: "../assets/js/app.js",
  bundle: true,
  format: "iife",
  target: "es2022",
  legalComments: "none",
  banner: { js: "// Built from web/src/app.ts by `npm run build` in web/. Don't edit by hand." },
  logLevel: "info",
});
