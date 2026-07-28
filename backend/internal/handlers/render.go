package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
)

var baseDir string
var dbPool *pgxpool.Pool

func SetBaseDir(dir string) {
	baseDir = dir
}

func SetDBPool(db *pgxpool.Pool) {
	dbPool = db
}

func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	if baseDir == "" {
		baseDir = "internal/templates"
	}

	funcMap := template.FuncMap{
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"seq":      func(n int) []int { s := make([]int, n); for i := 0; i < n; i++ { s[i] = i }; return s },
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}

	// Parse only base + the specific page template to avoid block name conflicts
	baseFile := filepath.Join(baseDir, "base.html")
	pageFile := filepath.Join(baseDir, "pages", name+".html")

	tmpl, err := template.New("").Funcs(funcMap).ParseFiles(baseFile, pageFile)
	if err != nil {
		log.Printf("[Template] Parse error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[Template] Execute error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
}