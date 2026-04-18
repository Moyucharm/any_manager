package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

type Renderer struct {
	pages map[string]*template.Template
	login *template.Template
}

var pageFiles = map[string]string{
	"dashboard": "templates/dashboard.html",
	"upstreams": "templates/upstreams.html",
	"settings":  "templates/settings.html",
	"logs":      "templates/logs.html",
}

func New() (*Renderer, error) {
	pages := make(map[string]*template.Template, len(pageFiles))
	for name, file := range pageFiles {
		tmpl, err := template.ParseFS(templatesFS, "templates/layout.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse page %q: %w", name, err)
		}
		pages[name] = tmpl
	}
	login, err := template.ParseFS(templatesFS, "templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("parse login template: %w", err)
	}
	return &Renderer{pages: pages, login: login}, nil
}

type PageData struct {
	Active string
	Title  string
}

func (r *Renderer) RenderPage(w http.ResponseWriter, name, title string) {
	tmpl, ok := r.pages[name]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", PageData{Active: name, Title: title}); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (r *Renderer) RenderLogin(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.login.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (r *Renderer) StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/admin/static/", http.FileServer(http.FS(sub)))
}
