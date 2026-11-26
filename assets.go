package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets/css/output.css assets/css/fonts.css assets/css/landing.css assets/css/api.css assets/js assets/app assets/front assets/fonts assets/img
var assetFS embed.FS

// assetVersion busts caches for the unhashed files (output.css, app.js) on each build.
var assetVersion = func() string {
	h := sha256.New()
	fs.WalkDir(assetFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := assetFS.ReadFile(path)
			h.Write(b)
		}
		return nil
	})
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
		// for good is fetched again on every page. A font that changes gets a new name.
		if strings.HasPrefix(r.URL.Path, "js/shadcn-templ-") || strings.HasPrefix(r.URL.Path, "fonts/") || r.URL.Query().Get("v") == assetVersion {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))
}
