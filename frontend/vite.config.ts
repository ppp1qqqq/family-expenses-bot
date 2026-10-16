import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// В dev-режиме запросы /api уходят на локальный бэкенд, в Docker это делает nginx.
// BASE задаёт путь, с которого раздаётся сборка: для GitHub Pages это /<репозиторий>/.
export default defineConfig({
  base: process.env.BASE ?? "/",
  plugins: [react()],
  server: {
    proxy: {
      "/api": process.env.API_URL ?? "http://localhost:8081",
    },
  },
});
