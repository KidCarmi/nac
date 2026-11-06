package server

import (
	"io/fs"
	"net/http"

	"github.com/kidcarmi/nac/web"
)

// uiCSP allows only same-origin scripts, styles and API calls: no inline
// code, no third-party origins, no framing.
const uiCSP = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// uiHandler serves the embedded operator console at /ui/.
func uiHandler() http.Handler {
	sub, err := fs.Sub(web.UI, "ui")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	files := http.StripPrefix("/ui/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", uiCSP)
		files.ServeHTTP(w, r)
	})
}
