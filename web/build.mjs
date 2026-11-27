// pdf.js's worker is copied into assets/js, which the binary embeds, where the app's
// agreement field editor loads it from: running Prospector needs no Node.
import { copyFile, mkdir } from "node:fs/promises";

await mkdir("../assets/js", { recursive: true });
await copyFile("node_modules/pdfjs-dist/build/pdf.worker.min.mjs", "../assets/js/pdf.worker.min.mjs");
await copyFile("node_modules/pdfjs-dist/LICENSE", "../assets/js/pdf.js.LICENSE.txt");
