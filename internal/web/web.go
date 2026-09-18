package web

import (
	"compress/gzip"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func Handler() http.Handler {
	return Gzip(http.FileServer(http.FS(Assets())))
}

func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || !gzippable(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(gzipWriter{ResponseWriter: w, w: gz}, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	w io.Writer
}

func (g gzipWriter) Write(p []byte) (int, error) {
	return g.w.Write(p)
}

func gzippable(path string) bool {
	path = strings.ToLower(path)
	for _, ext := range []string{".js", ".css", ".html", ".svg", ".json", ".txt", ".map"} {
		if strings.HasSuffix(path, ext) || path == "/" || path == "" {
			return true
		}
	}
	return path == "/" || !strings.Contains(path, ".")
}
