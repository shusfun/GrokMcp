/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/react-router")) return "router";
          if (id.includes("node_modules/react-dom") || id.includes("/node_modules/react/")) return "react";
          if (id.includes("@wailsio/runtime")) return "wails";
        },
      },
    },
  },
  test: {
    environment: "jsdom",
  },
});
