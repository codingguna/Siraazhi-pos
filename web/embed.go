/* ==========================================================================
   Package web — Embedded Static Asset Provider
   
   WHAT IT IS:
     This module embeds all frontend UI files (HTML, CSS, JavaScript, icons,
     manifest, and print stylesheets) directly inside the compiled Go binary.
     
   HOW IT WORKS:
     - Uses Go's standard `//go:embed static/*` compiler directive.
     - During `go build`, the compiler packs the entire `static/` directory
       into the executable image.
     - At runtime, `web.Handler()` wraps this embedded filesystem inside an `http.FileServer`.
     - Result: The user only needs a single file (`siraazhi-pos.exe`). There are
       ZERO loose HTML/CSS files to copy or maintain!

   DATA FLOW:
     Browser requests / or /css/style.css -> web.Handler() -> embedded binary FS -> HTTP Response
   ========================================================================== */

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
)

//go:embed static/*
var staticFS embed.FS

// Handler returns a standard http.Handler that serves the static files.
// If the local 'web/static' directory exists on disk (development/local running),
// it serves directly from disk so CSS/JS changes are reflected instantly without recompiling.
// When distributed as a standalone binary without loose assets, it seamlessly falls back to staticFS.
func Handler() http.Handler {
	if fi, err := os.Stat("web/static"); err == nil && fi.IsDir() {
		return http.FileServer(http.Dir("web/static"))
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

