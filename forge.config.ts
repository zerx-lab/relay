import type { ForgeConfig } from "@electron-forge/shared-types";
import { MakerZIP } from "@electron-forge/maker-zip";
import { MakerSquirrel } from "@electron-forge/maker-squirrel";
import { MakerAppImage } from "@reforged/maker-appimage";
import { VitePlugin } from "@electron-forge/plugin-vite";
import { AutoUnpackNativesPlugin } from "@electron-forge/plugin-auto-unpack-natives";
import { FusesPlugin } from "@electron-forge/plugin-fuses";
import { FuseV1Options, FuseVersion } from "@electron/fuses";

// Forge config for the Relay Electron shell.
//
// The Go backend is shipped as a sidecar binary under `bin/<platform-arch>/`
// and copied into the app's resources at package time via `extraResource`.
// Build that binary with `task build:go` before running `task build`.
const config: ForgeConfig = {
  packagerConfig: {
    asar: true,
    // `icon` is base path; packager appends .png/.ico/.icns per platform.
    icon: "./assets/logo/png/relay-512",
    extraResource: ["./bin", "./assets/logo"],
  },
  rebuildConfig: {},
  // Per-platform makers. Electron Forge invokes each maker only on its
  // declared platforms, so a single `task make` run on a given OS produces
  // exactly the artifacts that OS can build.
  //
  //   linux   -> AppImage (single self-contained executable)
  //   win32   -> Squirrel.Windows (RELEASES + .nupkg + Setup.exe; auto-update capable)
  //   darwin  -> ZIP (no signing identity wired in yet)
  //
  // ZIP is also kept as a portable fallback on linux/win32.
  makers: [
    new MakerAppImage({
      options: {
        // Forge auto-discovers icon/categories from `extraResource` + package.json,
        // but being explicit avoids surprises across maker versions.
        icon: "./assets/logo/png/relay-512.png",
        categories: ["Utility"],
      },
    }),
    new MakerSquirrel({
      // `name` becomes the Squirrel app id; must be a valid file/dir token.
      name: "relay",
      // setupIcon requires .ico; we only ship PNGs today, so fall back to
      // Forge's default until an .ico is added under assets/logo/.
    }),
    new MakerZIP({}, ["darwin", "linux", "win32"]),
  ],
  plugins: [
    new AutoUnpackNativesPlugin({}),
    new VitePlugin({
      build: [
        {
          entry: "electron/src/main/main.ts",
          config: "vite.main.config.ts",
          target: "main",
        },
        {
          entry: "electron/src/preload/preload.ts",
          config: "vite.preload.config.ts",
          target: "preload",
        },
      ],
      renderer: [
        {
          name: "main_window",
          config: "vite.renderer.config.ts",
        },
      ],
    }),
    // Recommended Electron hardening. See:
    // https://www.electronjs.org/docs/latest/tutorial/fuses
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: false,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};

export default config;
