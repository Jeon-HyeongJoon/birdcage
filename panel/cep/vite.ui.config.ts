import path from "path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  root: path.resolve(__dirname, "test", "ui"),
  resolve: {
    alias: {
      "@birdcage/core": path.resolve(__dirname, "..", "core", "src", "index.ts"),
    },
  },
  server: {
    host: "127.0.0.1",
    port: 4173,
    strictPort: true,
    fs: {
      allow: [__dirname, path.resolve(__dirname, "..", "core")],
    },
  },
});
