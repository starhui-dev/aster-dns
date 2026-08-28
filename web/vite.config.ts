import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";
import solid from "vite-plugin-solid";

const backendTarget = "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [solid(), tailwindcss()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: backendTarget,
        changeOrigin: false,
        headers: { host: "localhost:5173" },
      },
      "/healthz": {
        target: backendTarget,
        changeOrigin: false,
        headers: { host: "localhost:5173" },
      },
      "/readyz": {
        target: backendTarget,
        changeOrigin: false,
        headers: { host: "localhost:5173" },
      },
    },
  },
  build: {
    target: "es2022",
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
  },
});
