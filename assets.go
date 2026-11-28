package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// The whole directory, so Go builds before web/ has written assets/app and assets/js.
//
//go:embed assets
var assetFS embed.FS

// webBuilt reports whether the binary was built after the web app, which writes these.
func webBuilt(fsys fs.FS) error {
	for _, name := range []string{"app/index.html", "app/site.en-GB.html", "app/site.js", "app/api.html", "js/pdf.worker.min.mjs"} {
		if _, err := fs.Stat(fsys, "assets/"+name); err != nil {
			return fmt.Errorf("built without the web app (no assets/%s): run `npm ci && npm run build` in web/, then build again", name)
		}
	}
	if scripts, _ := fs.Glob(fsys, "assets/app/main-*.js"); len(scripts) == 0 {
		return fmt.Errorf("built without the web app (no assets/app/main-*.js): run `npm ci && npm run build` in web/, then build again")
	}
	return nil
}

// assetVersion busts caches for the unhashed files (output.css, the app) on each build.
var assetVersion = func() string {
	h := sha256.New()
	err := fs.WalkDir(assetFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := assetFS.ReadFile(path)
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		log.Printf("asset version: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}()

func assetHandler() http.Handler {
	sub, _ := fs.Sub(assetFS, "assets")
	files := http.FileServer(http.FS(sub))
	return http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".woff2") {
			// The release image has no system table of file types to find this in.
			w.Header().Set("Content-Type", "font/woff2")
		}
		// A worker runs under the policy its own script came with, not its page's.
		if r.URL.Path == "js/pdf.worker.min.mjs" {
			w.Header().Set("Content-Security-Policy", pdfPolicy)
		}
		// Embedded files carry no date to revalidate against, so anything not cached
		// for good is fetched again on every page. A font that changes gets a new name, and
		// so does each of the app's chunks, named for its content when it is built.
		if strings.HasPrefix(r.URL.Path, "fonts/") || strings.HasPrefix(r.URL.Path, "app/chunks/") || strings.HasPrefix(r.URL.Path, "app/main-") || r.URL.Query().Get("v") == assetVersion {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))
}
