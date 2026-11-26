import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The React pages build into assets/app, which the server embeds, as app.js does. Names
// are fixed so a page names its script plainly and the committed build diffs cleanly.
// Styles come from the shared Tailwind build (assets/css/output.css) while templ pages
// remain, so nothing here processes CSS.
export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  base: "/assets/app/",
  build: {
    outDir: "../assets/app",
    emptyOutDir: true,
    target: "es2022",
    cssCodeSplit: false,
    rollupOptions: {
      input: fileURLToPath(new URL("./src/main.tsx", import.meta.url)),
      output: {
        entryFileNames: "main.js",
        chunkFileNames: "chunks/[name].js",
        assetFileNames: "[name][extname]",
      },
    },
  },
});
