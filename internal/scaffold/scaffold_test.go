package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIgnoreAndMatch(t *testing.T) {
	src := `
# comment
cmd/zerx/
.scaffoldignore
/internal/scaffold/
`
	ig, err := ParseIgnore(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]bool{
		"cmd/zerx/main.go":           true,
		"cmd/zerxOther/main.go":      false,
		".scaffoldignore":            true,
		"internal/scaffold/fetch.go": true,
		"internal/server/server.go":  false,
		"main.go":                    false,
	}
	for path, want := range cases {
		if got := ig.Match(path); got != want {
			t.Errorf("Match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestReadModulePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/foo/bar\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadModulePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/foo/bar" {
		t.Errorf("got %q", got)
	}
}

func TestRewriteModule(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module github.com/old/proj\n\ngo 1.22\n",
		"main.go": `package main

import "github.com/old/proj/internal/server"

func main() { _ = server.X }
`,
		"sub/pkg.go":              "package sub\n// see github.com/old/proj/internal\n",
		"vendor/x/x.go":           `package x; const _ = "github.com/old/proj"`,
		"node_modules/y/index.js": `require("github.com/old/proj")`,
		"docs/README.md":          "refers to github.com/old/proj but not Go",
	})

	if err := RewriteModule(dir, "github.com/old/proj", "github.com/new/app"); err != nil {
		t.Fatal(err)
	}

	assertContains(t, filepath.Join(dir, "go.mod"), "module github.com/new/app")
	assertContains(t, filepath.Join(dir, "main.go"), `"github.com/new/app/internal/server"`)
	assertContains(t, filepath.Join(dir, "sub/pkg.go"), "github.com/new/app/internal")
	// vendor and node_modules left alone
	assertContains(t, filepath.Join(dir, "vendor/x/x.go"), "github.com/old/proj")
	assertContains(t, filepath.Join(dir, "node_modules/y/index.js"), "github.com/old/proj")
	// non-Go file untouched
	assertContains(t, filepath.Join(dir, "docs/README.md"), "github.com/old/proj")
}

func TestRewritePackageJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"relay","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RewritePackageJSON(dir, "myapp"); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(dir, "package.json"), `"name": "myapp"`)
	assertContains(t, filepath.Join(dir, "package.json"), `"version": "0.1.0"`)
}

func TestRewritePackageJSONMissingIsOK(t *testing.T) {
	if err := RewritePackageJSON(t.TempDir(), "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewIdentity(t *testing.T) {
	cases := []struct {
		in                 string
		wantOK             bool
		wantLow, wantTitle string
	}{
		{"myapp", true, "myapp", "Myapp"},
		{"my-app", true, "my-app", "My-app"},
		{"my_app2", true, "my_app2", "My_app2"},
		{"", false, "", ""},
		{"1app", false, "", ""},
		{"a/b", false, "", ""},
		{"a b", false, "", ""},
	}
	for _, c := range cases {
		got, err := NewIdentity(c.in)
		if (err == nil) != c.wantOK {
			t.Errorf("NewIdentity(%q) err=%v want ok=%v", c.in, err, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if got.Lower != c.wantLow || got.Title != c.wantTitle {
			t.Errorf("NewIdentity(%q) = %+v, want {%q,%q}", c.in, got, c.wantLow, c.wantTitle)
		}
	}
}

func TestRewriteIdentity(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		// Subset mirroring the real template surface. Each line is an
		// anchor for one rewrite rule.
		"Taskfile.yml": `vars:
  GO_BIN: relay-backend
  OTHER: keep-relay-untouched
`,
		"electron/src/main/backend.ts": `const exe = process.platform === "win32" ? "relay-backend.exe" : "relay-backend";
`,
		"electron/src/main/main.ts": `app.setName("relay");
ipcMain.handle("relay:handshake", () => {});
`,
		"electron/src/preload/preload.ts": `ipcRenderer.invoke("relay:handshake");
contextBridge.exposeInMainWorld("relay", api);
export type RelayBridge = typeof api;
`,
		"electron/src/api/client.ts": `declare global {
  interface Window {
    relay: {
      handshake(): Promise<void>;
    };
  }
}
const AUTH_HEADER = "X-Relay-Token";
const x = await window.relay.handshake();
`,
		"electron/src/renderer/renderer.ts": `const h = await window.relay.handshake();
`,
		"internal/server/server.go": `package server
const AuthHeader = "X-Relay-Token"
var _ = huma.DefaultConfig("Relay API2", "0.2.0")
`,
		"assets/relay.desktop": `[Desktop Entry]
Exec=relay %U
Icon=relay
StartupWMClass=relay
`,
		"forge.config.ts": `import { MakerSquirrel } from "@electron-forge/maker-squirrel";
const config = {
  makers: [
    new MakerSquirrel({
      name: "relay",
    }),
  ],
  plugins: [
    new VitePlugin({
      renderer: [{ name: "main_window", config: "" }],
    }),
  ],
};
`,
		// Files that must NOT be touched.
		"AGENTS.md":                    "the relay desktop scaffold\n",
		"assets/logo/relay-bridge.svg": `<svg><!-- relay --></svg>`,
	})

	id, err := NewIdentity("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if err := RewriteIdentity(dir, id); err != nil {
		t.Fatal(err)
	}

	assertContains(t, filepath.Join(dir, "Taskfile.yml"), "GO_BIN: myapp-backend")
	assertContains(t, filepath.Join(dir, "Taskfile.yml"), "OTHER: keep-relay-untouched")
	assertContains(t, filepath.Join(dir, "electron/src/main/backend.ts"), `"myapp-backend.exe"`)
	assertContains(t, filepath.Join(dir, "electron/src/main/backend.ts"), `"myapp-backend"`)
	assertContains(t, filepath.Join(dir, "electron/src/main/main.ts"), `app.setName("myapp")`)
	assertContains(t, filepath.Join(dir, "electron/src/main/main.ts"), `"myapp:handshake"`)
	assertContains(t, filepath.Join(dir, "electron/src/preload/preload.ts"), `exposeInMainWorld("myapp"`)
	assertContains(t, filepath.Join(dir, "electron/src/preload/preload.ts"), `MyappBridge`)
	assertContains(t, filepath.Join(dir, "electron/src/api/client.ts"), `myapp: {`)
	assertContains(t, filepath.Join(dir, "electron/src/api/client.ts"), `"X-Myapp-Token"`)
	assertContains(t, filepath.Join(dir, "electron/src/api/client.ts"), `window.myapp.handshake`)
	assertContains(t, filepath.Join(dir, "electron/src/renderer/renderer.ts"), `window.myapp.handshake`)
	assertContains(t, filepath.Join(dir, "internal/server/server.go"), `AuthHeader = "X-Myapp-Token"`)
	assertContains(t, filepath.Join(dir, "internal/server/server.go"), `huma.DefaultConfig("Myapp API2"`)
	assertContains(t, filepath.Join(dir, "assets/relay.desktop"), `Exec=myapp %U`)
	assertContains(t, filepath.Join(dir, "assets/relay.desktop"), `StartupWMClass=myapp`)
	assertContains(t, filepath.Join(dir, "assets/relay.desktop"), `Icon=relay`) // kept

	// forge.config.ts: MakerSquirrel name rewritten; unrelated `name:` for
	// the Vite renderer plugin entry is left alone.
	assertContains(t, filepath.Join(dir, "forge.config.ts"), `name: "myapp",`)
	assertContains(t, filepath.Join(dir, "forge.config.ts"), `name: "main_window"`)

	// Prose/comments untouched.
	assertContains(t, filepath.Join(dir, "AGENTS.md"), "the relay desktop scaffold")
	assertContains(t, filepath.Join(dir, "assets/logo/relay-bridge.svg"), "relay")
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertContains(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Errorf("%s missing %q\n--- content ---\n%s", path, want, raw)
	}
}
