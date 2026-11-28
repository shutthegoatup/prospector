import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The front page's script, built on its own after the app so that neither shares a
// chunk: the script is one file the server versions and browsers keep until the next
// release, and each language's messages a chunk named for its content.
export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir: "../assets/app",
    emptyOutDir: false,
    target: "es2022",
    rollupOptions: {
      input: fileURLToPath(new URL("./src/site/main.tsx", import.meta.url)),
      output: { entryFileNames: "site.js", chunkFileNames: "site-[name]-[hash].js" },
    },
  },
});
