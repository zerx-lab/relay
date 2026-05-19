import { defineConfig } from "vite";
import { resolve } from "node:path";

// Renderer (browser) build. Forge plugin discovers the `name: "main_window"`
// HTML file via the renderer config. Setting `root` to the renderer source
// folder lets Vite find `index.html` directly.
export default defineConfig({
  root: resolve(__dirname, "electron/src/renderer"),
});
