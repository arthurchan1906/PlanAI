import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "path";

export default defineConfig({
  root: resolve(__dirname),
  base: "./",
  plugins: [react()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      "/pmai": {
        target: "http://127.0.0.1:8011",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: resolve(__dirname, "dist"),
    emptyOutDir: true,
    commonjsOptions: {
      transformMixedEsModules: true,
    },
  },
});
