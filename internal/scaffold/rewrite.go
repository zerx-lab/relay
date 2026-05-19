package scaffold

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReadModulePath returns the module declaration from <root>/go.mod.
func ReadModulePath(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("scaffold: no module directive in go.mod")
}

// RewriteModule replaces every occurrence of oldMod with newMod across:
//   - go.mod (the module directive)
//   - every *.go file (import paths, fully-qualified references)
//
// Plain text substitution is sufficient because Go module paths are uniquely
// shaped strings; there is no risk of accidental matches inside comments or
// string literals at this scale. Vendor directories are skipped.
func RewriteModule(root, oldMod, newMod string) error {
	if oldMod == "" || newMod == "" {
		return errors.New("scaffold: empty module path")
	}
	if oldMod == newMod {
		return nil
	}
	oldBytes := []byte(oldMod)
	newBytes := []byte(newMod)

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Standard "do not rewrite" directories.
			if name == "vendor" || name == "node_modules" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name != "go.mod" && !strings.HasSuffix(name, ".go") {
			return nil
		}
		return replaceInFile(path, oldBytes, newBytes)
	})
}

// RewritePackageJSON sets the "name" field of <root>/package.json to name.
// Missing file is not an error (the template may not ship one in the future).
func RewritePackageJSON(root, name string) error {
	path := filepath.Join(root, "package.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Decode into a map to preserve every other field. json.Marshal sorts
	// keys alphabetically, which differs from the source order, but for a
	// generated project that's acceptable.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("scaffold: parse package.json: %w", err)
	}
	obj["name"] = name
	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func replaceInFile(path string, oldB, newB []byte) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Contains(raw, oldB) {
		return nil
	}
	updated := bytes.ReplaceAll(raw, oldB, newB)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, info.Mode().Perm())
}
