import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Renders the public site's pages for prerender.mjs: a module run while building,
// never served.
export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    ssr: fileURLToPath(new URL("./src/site/render.tsx", import.meta.url)),
    outDir: "site-build",
    emptyOutDir: true,
    rollupOptions: { output: { entryFileNames: "render.js" } },
  },
});
