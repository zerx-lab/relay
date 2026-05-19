// Command zerx scaffolds a new project from the zerx-lab/relay template.
//
// Install:
//
//	go install github.com/zerx-lab/relay/cmd/zerx@latest
//
// Usage:
//
//	zerx new <name> --module <go-module-path> [--ref vX.Y.Z]
//	zerx version
//
// The template source is the release tarball of the upstream repository
// (default: latest release). The CLI's own files (cmd/zerx, internal/scaffold,
// .scaffoldignore) are stripped from the extracted tree before rewriting the
// Go module path and package.json name.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zerx-lab/relay/internal/scaffold"
)

// Pinned upstream. The CLI is purpose-built for this template; making the
// source configurable would invite drift between rewrite logic and template
// layout.
var upstream = scaffold.Source{Owner: "zerx-lab", Repo: "relay"}

// version is overwritten at build time via -ldflags. Default keeps `go install`
// builds informative.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "new":
		if err := runNew(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "zerx:", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("zerx", version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "zerx: unknown command %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	_, _ = fmt.Fprint(w, `zerx - scaffold a new project from the zerx-lab/relay template

Usage:
  zerx new <name> --module <go-module-path> [--ref vX.Y.Z]
  zerx version
  zerx help

Flags for "new":
  --module   Go module path of the generated project (e.g. github.com/you/app).
             Prompted interactively when omitted.
  --ref      Release tag to materialize. Defaults to the latest release.
`)
}

func runNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		modulePath string
		ref        string
	)
	fs.StringVar(&modulePath, "module", "", "Go module path of the new project")
	fs.StringVar(&ref, "ref", "", "release tag to materialize (default: latest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New(`usage: zerx new <name> --module <go-module-path> [--ref vX.Y.Z]`)
	}
	name := fs.Arg(0)
	identity, err := scaffold.NewIdentity(name)
	if err != nil {
		return err
	}

	if modulePath == "" {
		got, err := promptModule(name)
		if err != nil {
			return err
		}
		modulePath = got
	}
	if err := validateModulePath(modulePath); err != nil {
		return err
	}

	dst, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination %q already exists", dst)
	} else if !os.IsNotExist(err) {
		return err
	}

	// 1. Resolve release.
	releaseRef := scaffold.Latest
	if ref != "" {
		releaseRef = scaffold.ReleaseRef(ref)
	}
	fmt.Fprintf(os.Stderr, "Resolving release (%s)...\n", releaseRef)
	tarURL, tag, err := scaffold.Resolve(upstream, releaseRef)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  -> %s\n", tag)

	// 2. Extract into a sibling temp dir so a failure can't leave a half-built
	// project at the target path.
	tmp, err := os.MkdirTemp(filepath.Dir(dst), ".zerx-"+name+"-*")
	if err != nil {
		return err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()

	fmt.Fprintln(os.Stderr, "Downloading and extracting...")
	ig, err := loadIgnoreFromTarball(tarURL)
	if err != nil {
		return err
	}
	if err := scaffold.ExtractTarballURL(tarURL, tmp, ig); err != nil {
		return err
	}

	// 3. Rewrite module path + package.json name.
	oldMod, err := scaffold.ReadModulePath(tmp)
	if err != nil {
		return fmt.Errorf("read template go.mod: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Rewriting module path %s -> %s\n", oldMod, modulePath)
	if err := scaffold.RewriteModule(tmp, oldMod, modulePath); err != nil {
		return err
	}
	if err := scaffold.RewritePackageJSON(tmp, name); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Rewriting project identity (relay -> "+identity.Lower+")...")
	if err := scaffold.RewriteIdentity(tmp, identity); err != nil {
		return err
	}

	// 4. Move tmp -> dst (same parent dir, so rename is atomic).
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	cleanupTmp = false

	fmt.Fprintf(os.Stderr, "\nCreated %s (from %s)\n", dst, tag)
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintf(os.Stderr, "  cd %s\n", name)
	fmt.Fprintln(os.Stderr, "  task install   # pnpm install")
	fmt.Fprintln(os.Stderr, "  task dev       # run Electron + Go sidecar")
	return nil
}

// loadIgnoreFromTarball does a *second* tarball pass purely to read the
// .scaffoldignore file shipped inside the template. Doing it this way keeps
// the ignore list versioned with the template (an old CLI generating a new
// release will still strip the right files).
//
// The double download cost is acceptable: source tarballs are small (single
// digit MB) and this command is interactive.
func loadIgnoreFromTarball(url string) (*scaffold.Ignorer, error) {
	tmp, err := os.MkdirTemp("", "zerx-ignore-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := scaffold.ExtractTarballURL(url, tmp, nil); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(tmp, ".scaffoldignore"))
	if err != nil {
		if os.IsNotExist(err) {
			// Older releases may pre-date the file. Fall back to no filtering;
			// the CLI sources will simply ship inside the generated project.
			return scaffold.ParseIgnore(strings.NewReader(""))
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return scaffold.ParseIgnore(f)
}

func promptModule(name string) (string, error) {
	suggestion := "github.com/you/" + name
	fmt.Fprintf(os.Stderr, "Go module path [%s]: ", suggestion)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return suggestion, nil
	}
	got := strings.TrimSpace(scanner.Text())
	if got == "" {
		return suggestion, nil
	}
	return got, nil
}

func validateModulePath(p string) error {
	if p == "" {
		return errors.New("module path required")
	}
	if strings.ContainsAny(p, " \t\n") {
		return fmt.Errorf("invalid module path %q", p)
	}
	return nil
}
