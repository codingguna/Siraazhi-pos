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
)

//go:embed static/*
var staticFS embed.FS

// Handler returns a standard http.Handler that serves the embedded static files.
// Flow: creates sub-filesystem rooted at 'static' -> returns standard http.FileServer.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
