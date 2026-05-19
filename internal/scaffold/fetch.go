package scaffold

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Source identifies the upstream template repository. It is wired by the CLI
// (cmd/zerx) at call time rather than baked in here so tests can point at a
// fixture server.
type Source struct {
	Owner string // e.g. "zerx-lab"
	Repo  string // e.g. "relay"
}

// ReleaseRef is a release tag like "v0.1.0". The "latest" sentinel resolves
// to /releases/latest. Branches are intentionally not supported (see AGENTS).
type ReleaseRef string

const Latest ReleaseRef = "latest"

// httpClient is module-private so tests can swap it. A short timeout prevents
// `zerx new` from hanging on a flaky network.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// Resolve returns the tarball URL for the given ref. For Latest it queries
// /releases/latest; otherwise /releases/tags/<ref>.
func Resolve(src Source, ref ReleaseRef) (tarballURL, resolvedTag string, err error) {
	if src.Owner == "" || src.Repo == "" {
		return "", "", errors.New("scaffold: source owner/repo required")
	}
	var url string
	if ref == "" || ref == Latest {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", src.Owner, src.Repo)
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", src.Owner, src.Repo, string(ref))
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub serves anonymous public-repo requests at 60/hour, which is plenty
	// for a scaffold CLI. If we ever hit the limit, add GITHUB_TOKEN handling.

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("scaffold: GitHub API %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		TagName    string `json:"tag_name"`
		TarballURL string `json:"tarball_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("scaffold: decode release: %w", err)
	}
	if payload.TarballURL == "" {
		return "", "", errors.New("scaffold: release has no tarball_url")
	}
	return payload.TarballURL, payload.TagName, nil
}

// ExtractTarballURL streams the gzipped tarball at url into dst, stripping the
// single GitHub-injected top-level directory ("owner-repo-<sha>/") and any
// entries matched by ig.
//
// Symlinks, hardlinks and absolute / escaping paths are rejected to keep the
// extraction safe to point at any user-chosen directory.
func ExtractTarballURL(url, dst string, ig *Ignorer) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("scaffold: download %s: %s", url, resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("scaffold: gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	return extractTar(tar.NewReader(gz), dst, ig)
}

func extractTar(tr *tar.Reader, dst string, ig *Ignorer) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("scaffold: tar: %w", err)
		}

		// GitHub wraps everything in "<owner>-<repo>-<sha>/". Strip the first
		// component; entries with no separator are the wrapper dir itself.
		clean := path.Clean(hdr.Name)
		slash := strings.IndexByte(clean, '/')
		if slash < 0 {
			continue
		}
		rel := clean[slash+1:]
		if rel == "" || rel == "." {
			continue
		}
		// Guard against tar entries trying to climb out via "..".
		if strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || rel == ".." {
			return fmt.Errorf("scaffold: refusing path with ..: %q", hdr.Name)
		}
		if ig.Match(rel) {
			continue
		}

		target := filepath.Join(dst, filepath.FromSlash(rel))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeFile(target, tr, mode); err != nil {
				return err
			}
		default:
			// Skip symlinks, devices, etc. The scaffold doesn't need them and
			// allowing them widens the attack surface.
			continue
		}
	}
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
