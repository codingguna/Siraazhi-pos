/* ==========================================================================
   Package web — Embedded Static Asset Provider & HTML Partial Assembler

   WHAT IT IS:
     Modular frontend pipeline that embeds all web assets inside the binary
     while supporting professional multi-file page architectures:
     - Separates each application view into individual page files:
         • web/static/pages/pos.html
         • web/static/pages/dash.html
         • web/static/pages/inventory.html
         • web/static/pages/production.html
         • web/static/pages/purchases.html
         • web/static/pages/expenses.html
         • web/static/pages/bills.html
         • web/static/pages/gstr1.html
         • web/static/pages/settings.html
         • web/static/pages/modals.html
     - Dynamically assembles the master index.html using <!-- INCLUDE: ... --> directives.
     - Live reloads partials directly from disk during development without recompilation.
     - Embeds and serves high-performance cached builds in production binaries.
   ========================================================================== */

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

//go:embed static/*
var staticFS embed.FS

var (
	includeRegex  = regexp.MustCompile(`<!--\s*INCLUDE:\s*([^\s>]+)\s*-->`)
	cachedHTML    []byte
	cacheMu       sync.RWMutex
	isDevModeOnce sync.Once
	isDevMode     bool
)

func checkDevMode() bool {
	isDevModeOnce.Do(func() {
		// If web/static/index.html exists on local disk, enable live reloading
		if fi, err := os.Stat(filepath.Join("web", "static", "index.html")); err == nil && !fi.IsDir() {
			isDevMode = true
		}
	})
	return isDevMode
}

// readAsset reads file content from local disk if in dev mode, or falls back to staticFS.
func readAsset(relPath string) ([]byte, error) {
	relPath = filepath.Clean(relPath)
	relPath = strings.TrimPrefix(relPath, "static/")
	relPath = strings.TrimPrefix(relPath, "static\\")

	if checkDevMode() {
		diskPath := filepath.Join("web", "static", filepath.FromSlash(relPath))
		if data, err := os.ReadFile(diskPath); err == nil {
			return data, nil
		}
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(sub, filepath.ToSlash(relPath))
}

// AssembleIndexHTML combines the base index.html template with all partial page files.
func AssembleIndexHTML() ([]byte, error) {
	if !checkDevMode() {
		cacheMu.RLock()
		if len(cachedHTML) > 0 {
			res := cachedHTML
			cacheMu.RUnlock()
			return res, nil
		}
		cacheMu.RUnlock()
	}

	baseContent, err := readAsset("index.html")
	if err != nil {
		return nil, err
	}

	assembled := includeRegex.ReplaceAllFunc(baseContent, func(match []byte) []byte {
		m := includeRegex.FindSubmatch(match)
		if len(m) < 2 {
			return match
		}
		partialFile := strings.TrimSpace(string(m[1]))
		partialData, err := readAsset(partialFile)
		if err != nil {
			return []byte("<!-- ERROR: missing partial " + partialFile + ": " + err.Error() + " -->")
		}
		return partialData
	})

	if !checkDevMode() {
		cacheMu.Lock()
		cachedHTML = assembled
		cacheMu.Unlock()
	}

	return assembled, nil
}

// Handler returns an http.Handler that dynamically serves the assembled HTML for root/index,
// and static file assets for all other routes.
// If the local 'web/static' directory exists on disk (development/local running),
// it serves directly from disk so CSS/JS changes are reflected instantly without recompiling.
// When distributed as a standalone binary without loose assets, it seamlessly falls back to staticFS.
func Handler() http.Handler {
	var fileServer http.Handler
	if fi, err := os.Stat("web/static"); err == nil && fi.IsDir() {
		fileServer = http.FileServer(http.Dir("web/static"))
	} else {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			panic(err)
		}
		fileServer = http.FileServer(http.FS(sub))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "" || path == "/" || path == "/index.html" {
			html, err := AssembleIndexHTML()
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(html)
				return
			}
		}

		if path == "/sw.js" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		fileServer.ServeHTTP(w, r)
	})
}


