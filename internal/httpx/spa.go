package httpx

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type SPAHandler struct {
	root      string
	indexFile string
}

func NewSPAHandler(root string) *SPAHandler {
	return &SPAHandler{
		root:      filepath.Clean(root),
		indexFile: filepath.Join(filepath.Clean(root), "index.html"),
	}
}

func (handler *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed.", nil)
		return
	}

	relativePath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	candidate := filepath.Join(handler.root, filepath.FromSlash(relativePath))
	if isInsideRoot(handler.root, candidate) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if strings.HasPrefix(relativePath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFile(w, r, candidate)
			return
		}
	}

	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, handler.indexFile)
}

func isInsideRoot(root, candidate string) bool {
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}
