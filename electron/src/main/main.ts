import { app, BrowserWindow, ipcMain, nativeImage } from "electron";
import { type FSWatcher, watch as fsWatch, statSync } from "node:fs";
import { join } from "node:path";
import { startBackend, type Backend } from "./backend";

// These globals are injected by @electron-forge/plugin-vite at build time.
declare const MAIN_WINDOW_VITE_DEV_SERVER_URL: string | undefined;
declare const MAIN_WINDOW_VITE_NAME: string;

// Pin the app name so Wayland's xdg_toplevel.app_id is stable ("relay").
// The installed `relay.desktop` matches on StartupWMClass=relay.
app.setName("relay");

let backend: Backend | null = null;
let reloading = false;

const RELOAD_FILE = join(app.getAppPath(), ".dev-reload");

/**
 * Dev-only hot restart triggered when scripts/dev-watcher.mjs touches the
 * `.dev-reload` trigger file after a successful Go rebuild. Respawns the
 * sidecar — the new handshake carries fresh port/token — then reloads
 * renderers so the cached API client is rebuilt.
 *
 * The Go rebuild itself is owned by the watcher, NOT this function: keeping
 * orchestration in one place (a Node script invoking `task`) avoids running
 * the toolchain inside Electron's main process.
 */
async function reloadBackend() {
  if (reloading) return;
  reloading = true;
  try {
    process.stderr.write("[dev] restarting Go sidecar...\n");
    backend?.stop();
    backend = await startBackend();
    process.stderr.write(
      `[dev] sidecar up on ${backend.handshake.baseUrl}\n`,
    );
    for (const win of BrowserWindow.getAllWindows()) {
      win.webContents.reloadIgnoringCache();
    }
  } catch (err) {
    process.stderr.write(`[dev] restart failed: ${String(err)}\n`);
  } finally {
    reloading = false;
  }
}

/**
 * Watch `.dev-reload`; any write (mtime change) triggers reloadBackend().
 * Cross-platform: works the same on Linux (inotify), macOS (FSEvents) and
 * Windows (ReadDirectoryChangesW). No POSIX signals involved.
 *
 * Implementation note: fs.watch fires on a file only if the file exists at
 * watch-start time. We watch the parent directory and filter by filename so
 * the trigger file can be created on the fly by the watcher script.
 */
function installDevReloader() {
  if (app.isPackaged) return;
  const dir = app.getAppPath();
  let lastMtimeMs = 0;
  let fsWatcher: FSWatcher;
  try {
    fsWatcher = fsWatch(dir, { persistent: false }, (_event, filename) => {
      if (filename !== ".dev-reload") return;
      // Coalesce duplicate events (rename+change on Windows, two changes on
      // Linux atomic writes) by debouncing on mtime.
      try {
        const { mtimeMs } = statSync(RELOAD_FILE);
        if (mtimeMs === lastMtimeMs) return;
        lastMtimeMs = mtimeMs;
      } catch {
        return; // file vanished between event and stat
      }
      void reloadBackend();
    });
  } catch (err) {
    process.stderr.write(
      `[dev] cannot watch ${dir} for hot restart: ${String(err)}\n`,
    );
    return;
  }
  app.on("before-quit", () => fsWatcher.close());
  process.stderr.write(
    "[dev] hot-restart armed (watching .dev-reload). Save any *.go to trigger.\n",
  );
}

// Resolve the window icon. In dev the working dir is the repo root, in a
// packaged app the PNGs live next to the asar under `resources/assets/`.
function resolveIcon() {
  const candidates = [
    join(__dirname, "../../assets/logo/png/relay-256.png"),
    join(process.resourcesPath ?? "", "assets/logo/png/relay-256.png"),
    join(process.cwd(), "assets/logo/png/relay-256.png"),
  ];
  for (const p of candidates) {
    const img = nativeImage.createFromPath(p);
    if (!img.isEmpty()) return img;
  }
  return undefined;
}

async function createWindow() {
  const win = new BrowserWindow({
    width: 1100,
    height: 720,
    icon: resolveIcon(),
    webPreferences: {
      preload: join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });

  if (MAIN_WINDOW_VITE_DEV_SERVER_URL) {
    await win.loadURL(MAIN_WINDOW_VITE_DEV_SERVER_URL);
  } else {
    await win.loadFile(
      join(__dirname, `../renderer/${MAIN_WINDOW_VITE_NAME}/index.html`),
    );
    // NB: Forge's Vite plugin places renderer output one level above the
    // main process bundle (see `.vite/renderer/<name>/`).
  }
}

app.whenReady().then(async () => {
  backend = await startBackend();

  // Expose the handshake to the renderer through an IPC channel. Preload
  // calls this synchronously on startup; the renderer never sees Node APIs.
  // Re-reads `backend` on every call so post-reload renderers get the
  // fresh port/token.
  ipcMain.handle("relay:handshake", () => {
    if (!backend) throw new Error("backend not started");
    return backend.handshake;
  });

  installDevReloader();

  await createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});

app.on("before-quit", () => {
  backend?.stop();
  backend = null;
});
