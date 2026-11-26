import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The app builds into assets/app, which the server embeds: index.html is the page the
// server answers every app address with, and main.js its script. Names are fixed so the
// committed build diffs cleanly; the server adds the version to them. Styles come from
// the shared Tailwind build (assets/css/output.css), so nothing here processes CSS.
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
      input: fileURLToPath(new URL("./index.html", import.meta.url)),
      output: {
        entryFileNames: "main.js",
        chunkFileNames: "chunks/[name].js",
        assetFileNames: "[name][extname]",
      },
    },
  },
});
