package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed index.html
var indexHTML []byte

//go:embed 404.html
var notFoundHTML []byte

//go:embed assets/*
var assetsFS embed.FS

//go:embed robots.txt sitemap.xml llms.txt favicon.ico
var seoFS embed.FS

func init() {
	// not in every system mime table, and video needs the right type
	for ext, typ := range map[string]string{".woff2": "font/woff2", ".mp4": "video/mp4", ".webm": "video/webm"} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(indexHTML)
	})

	// crawler files at root, served from the embedded source so they match the
	// static copies CF Pages serves in production (see build.sh)
	seoFile := func(name, ctype string) http.HandlerFunc {
		body, _ := seoFS.ReadFile(name)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			_, _ = w.Write(body)
		}
	}
	r.Get("/robots.txt", seoFile("robots.txt", "text/plain; charset=utf-8"))
	r.Get("/sitemap.xml", seoFile("sitemap.xml", "application/xml; charset=utf-8"))
	r.Get("/llms.txt", seoFile("llms.txt", "text/plain; charset=utf-8"))
	r.Get("/favicon.ico", seoFile("favicon.ico", "image/x-icon"))

	// a real 404, so stray urls are not indexed as copies of the home page
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(notFoundHTML)
	})

	assetsSub, _ := fs.Sub(assetsFS, "assets")
	files := http.StripPrefix("/assets", http.FileServer(http.FS(assetsSub)))
	r.Handle("/assets/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))

	fmt.Printf("🐝 bee website, http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
