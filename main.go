// Command relay is the Go backend for the Relay Electron app.
//
// Subcommands:
//
//	(no args)         start the HTTP server on 127.0.0.1, print a JSON
//	                  handshake line to stdout, then block.
//	openapi [path]    dump the OpenAPI 3.1 schema as YAML and exit. The
//	                  default path is "openapi.yaml" relative to the cwd.
//
// The handshake line printed on startup looks like:
//
//	{"port":54321,"token":"<hex>","baseUrl":"http://127.0.0.1:54321"}
//
// Electron's main process reads the first stdout line, parses it, and exposes
// baseUrl + token to the renderer through preload. The renderer then uses
// openapi-fetch to call the Go API with full type safety.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/zerx-lab/relay/internal/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		path := "openapi.yaml"
		if len(os.Args) > 2 {
			path = os.Args[2]
		}
		if err := dumpOpenAPI(path); err != nil {
			fmt.Fprintln(os.Stderr, "openapi dump failed:", err)
			os.Exit(1)
		}
		_, _ = fmt.Fprintln(os.Stdout, "wrote", path)
		return
	}

	if err := runServer(); err != nil {
		fmt.Fprintln(os.Stderr, "server failed:", err)
		os.Exit(1)
	}
}

func runServer() error {
	built, err := server.Build(server.Config{})
	if err != nil {
		return err
	}

	addr := built.Listener.Addr().(*net.TCPAddr)
	handshake := map[string]any{
		"port":    addr.Port,
		"token":   built.Token,
		"baseUrl": fmt.Sprintf("http://%s:%d", server.LoopbackHost, addr.Port),
	}
	if err := json.NewEncoder(os.Stdout).Encode(handshake); err != nil {
		return err
	}

	return http.Serve(built.Listener, built.Handler)
}

func dumpOpenAPI(path string) error {
	built, err := server.Build(server.Config{Token: "dump"}) // token unused, listener closed immediately
	if err != nil {
		return err
	}
	_ = built.Listener.Close()

	yaml, err := built.API.OpenAPI().YAML()
	if err != nil {
		return err
	}
	return os.WriteFile(path, yaml, 0o644)
}
