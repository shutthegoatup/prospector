// pdf.js's worker is copied into the committed assets/js, where the app's agreement
// field editor loads it from: running Prospector needs no Node.
import { copyFile } from "node:fs/promises";

await copyFile("node_modules/pdfjs-dist/build/pdf.worker.min.mjs", "../assets/js/pdf.worker.min.mjs");
await copyFile("node_modules/pdfjs-dist/LICENSE", "../assets/js/pdf.js.LICENSE.txt");
