package scaffold

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode"
)

// Identity captures the project-name-derived strings sprinkled across the
// template. The CLI fills these in from the user-supplied <name>.
//
// We deliberately do NOT touch:
//   - File names (relay-*.png, relay-bridge.svg, relay.desktop). Renaming
//     files would require simultaneous edits in many references and the
//     payoff (cosmetic) doesn't justify the surface area.
//   - Free-form text in AGENTS.md / comments. "relay" is a common English
//     word; substring replacement there would mangle prose.
//
// Substitution is anchored at well-known *literal* call-sites (Taskfile
// variable, app.setName argument, IPC channel name, etc.). Each rewrite is
// a single exact-string replacement scoped to a specific file or extension
// allowlist.
type Identity struct {
	Lower string // e.g. "myapp"
	Title string // e.g. "Myapp" - first letter upper-cased, rest as-is
}

// NewIdentity validates name and derives the Title form.
//
// Constraints:
//   - non-empty
//   - first character ASCII letter (so generated TS class/header names are
//     valid identifiers)
//   - remaining characters: ASCII letter, digit, '-' or '_'
//
// These rules match what npm package names and most cross-platform binary
// names accept simultaneously.
func NewIdentity(name string) (Identity, error) {
	if name == "" {
		return Identity{}, errors.New("project name required")
	}
	first := rune(name[0])
	if !isASCIILetter(first) {
		return Identity{}, errors.New("project name must start with an ASCII letter")
	}
	for _, r := range name[1:] {
		if !isASCIILetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return Identity{}, errors.New("project name may contain letters, digits, '-' and '_' only")
		}
	}
	return Identity{
		Lower: name,
		Title: strings.ToUpper(name[:1]) + name[1:],
	}, nil
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// RewriteIdentity applies the per-file, per-literal substitutions for the
// chosen project name. It must be called AFTER RewriteModule (the two
// rewrites are independent but running module first keeps debug output
// readable when something goes wrong).
//
// Old identity is hard-coded to "relay" / "Relay" because this CLI is
// purpose-built for the zerx-lab/relay template; making it configurable
// would mask the very real coupling between rewrite rules and template
// layout.
func RewriteIdentity(root string, id Identity) error {
	rules := identityRules(id)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, rule := range rules {
			if !rule.matches(rel) {
				continue
			}
			if err := replaceLiteralInFile(path, rule.old, rule.new); err != nil {
				return err
			}
		}
		return nil
	})
}

// rewriteRule replaces literal `old` with literal `new` in any file whose
// repo-relative slash path satisfies pathPred.
type rewriteRule struct {
	old, new string
	pathPred func(rel string) bool
}

func (r rewriteRule) matches(rel string) bool { return r.pathPred(rel) }

func identityRules(id Identity) []rewriteRule {
	lower := id.Lower
	title := id.Title

	tsFile := func(rel string) bool {
		return strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")
	}
	goFile := func(rel string) bool { return strings.HasSuffix(rel, ".go") }
	exact := func(want string) func(string) bool {
		return func(rel string) bool { return rel == want }
	}

	return []rewriteRule{
		// --- Go binary name ---
		// Taskfile GO_BIN var. Anchored to the YAML literal so we don't catch
		// the word "relay-backend" elsewhere by accident.
		{
			old:      "GO_BIN: relay-backend",
			new:      "GO_BIN: " + lower + "-backend",
			pathPred: exact("Taskfile.yml"),
		},
		// Electron sidecar resolver references the same binary name.
		{
			old:      `"relay-backend.exe"`,
			new:      `"` + lower + `-backend.exe"`,
			pathPred: exact("electron/src/main/backend.ts"),
		},
		{
			old:      `"relay-backend"`,
			new:      `"` + lower + `-backend"`,
			pathPred: exact("electron/src/main/backend.ts"),
		},

		// --- Electron app identity ---
		{
			old:      `app.setName("relay")`,
			new:      `app.setName("` + lower + `")`,
			pathPred: exact("electron/src/main/main.ts"),
		},

		// --- IPC channel + window.<name> bridge ---
		// "relay:handshake" appears in main.ts (ipcMain.handle) and
		// preload.ts (ipcRenderer.invoke). The colon makes the literal
		// unique enough that we don't need per-file scoping, but we keep
		// it scoped to TS files to be safe.
		{
			old:      `"relay:handshake"`,
			new:      `"` + lower + `:handshake"`,
			pathPred: tsFile,
		},
		// contextBridge.exposeInMainWorld("relay", api) — preload only.
		{
			old:      `exposeInMainWorld("relay"`,
			new:      `exposeInMainWorld("` + lower + `"`,
			pathPred: exact("electron/src/preload/preload.ts"),
		},
		// window.relay -> window.<lower>. Limited to TS files where the
		// bridge is consumed. The token is "window.relay" + word break to
		// avoid eating "window.relayX" if such a symbol ever existed.
		{
			old:      `window.relay.handshake`,
			new:      `window.` + lower + `.handshake`,
			pathPred: tsFile,
		},
		// Augmented Window interface in client.ts.
		{
			old:      `    relay: {`,
			new:      `    ` + lower + `: {`,
			pathPred: exact("electron/src/api/client.ts"),
		},
		// RelayBridge type name in preload.ts.
		{
			old:      `RelayBridge`,
			new:      title + `Bridge`,
			pathPred: exact("electron/src/preload/preload.ts"),
		},

		// --- Auth header (Go server side <-> TS client side contract) ---
		// Go const.
		{
			old:      `AuthHeader = "X-Relay-Token"`,
			new:      `AuthHeader = "X-` + title + `-Token"`,
			pathPred: exact("internal/server/server.go"),
		},
		// TS const.
		{
			old:      `AUTH_HEADER = "X-Relay-Token"`,
			new:      `AUTH_HEADER = "X-` + title + `-Token"`,
			pathPred: exact("electron/src/api/client.ts"),
		},

		// --- Huma API title ---
		// `huma.DefaultConfig("Relay API2", "0.2.0")`. The "API2" suffix
		// looks like a typo in the upstream template; we preserve user
		// intent by keeping the shape and only swapping the leading word.
		{
			old:      `huma.DefaultConfig("Relay API2"`,
			new:      `huma.DefaultConfig("` + title + ` API2"`,
			pathPred: goFile,
		},

		// --- .desktop file body ---
		// File NAME stays "relay.desktop" by user choice. Inside, the
		// Exec / Icon / StartupWMClass fields must agree with app.setName
		// so Wayland matches the window.
		{
			old:      "Exec=relay %U",
			new:      "Exec=" + lower + " %U",
			pathPred: exact("assets/relay.desktop"),
		},
		{
			old:      "StartupWMClass=relay",
			new:      "StartupWMClass=" + lower,
			pathPred: exact("assets/relay.desktop"),
		},
		// Note: Icon=relay deliberately left as-is because the installed
		// hicolor PNGs are still named "relay.png" (user kept file names).
	}
}

// replaceLiteralInFile is the no-op-safe single-string replacement helper
// shared by all identity rules.
func replaceLiteralInFile(path, oldStr, newStr string) error {
	return replaceInFile(path, []byte(oldStr), []byte(newStr))
}
