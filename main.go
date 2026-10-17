// Command nodestatus is a mikan protocol-2 addon: it probes VPN nodes,
// keeps availability history in the panel KV store, shows an admin
// dashboard and publishes a public status page to GitHub Pages.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	listenAddr = os.Getenv("MIKAN_ADAPTER_LISTEN")
	adapterTok = os.Getenv("MIKAN_ADAPTER_TOKEN")
	panelURL   = os.Getenv("MIKAN_PANEL_URL")
	panelTok   = os.Getenv("MIKAN_PANEL_TOKEN")
)

func main() {
	if listenAddr == "" || adapterTok == "" || panelURL == "" || panelTok == "" {
		log.Fatal("missing required env: MIKAN_ADAPTER_LISTEN, MIKAN_ADAPTER_TOKEN, MIKAN_PANEL_URL, MIKAN_PANEL_TOKEN")
	}
	kv := NewKV(strings.TrimSuffix(panelURL, "/"), panelTok)

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/info", withAuth(handleInfo))
	mux.HandleFunc("/v2/health", withAuth(handleHealth))
	mux.HandleFunc("/v2/check", withAuth(handleCheck))
	mux.HandleFunc("/ui/", withAuth(handleUI))
	mux.HandleFunc("/v2/ui/rpc", withAuth(handleRPC(kv)))
	mux.HandleFunc("/status", handlePublicStatus(kv))

	go proberLoop(kv)

	// Public status server: serves only the status page, no auth, no admin
	// endpoints. Lets the addon serve users directly without a reverse proxy.
	// The panel-facing server above stays on loopback with auth.
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeJSON(w, 404, map[string]string{"code": "not_found"})
			return
		}
		handlePublicStatus(kv)(w, r)
	})
	go func() {
		psrv := &http.Server{
			Addr:         "0.0.0.0:8080",
			Handler:      publicMux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 20 * time.Second,
		}
		log.Printf("nodestatus: public status on %s", psrv.Addr)
		if err := psrv.ListenAndServe(); err != nil {
			log.Printf("nodestatus: public server stopped: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 40 * time.Second,
	}
	log.Printf("nodestatus: listening on %s", listenAddr)
	log.Fatal(srv.ListenAndServe())
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer " + adapterTok
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "unauthorized", "message": "bad token"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleCheck validates settings posted by the panel. We keep settings in
// our own admin UI (KV), so the native form is empty and this is a no-op.
func handleCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"code": "invalid", "message": "method not allowed"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":          adapterID,
		"protocol":    2,
		"version":     adapterVersion,
		"kind":        "ui",
		"name":        map[string]string{"ru": "Статус нод", "en": "Node status"},
		"description": map[string]string{"ru": "Мониторинг нод: проверки доступности, аптайм, публичная страница статуса", "en": "Node monitoring: availability checks, uptime, public status page"},
		"permissions": []string{"kv", "ui"},
		"settings":    []any{},
		"ui": map[string]any{
			"entry": "/ui/index.html",
			"title": map[string]string{"ru": "Статус нод", "en": "Node status"},
		},
		"help": map[string]string{
			"ru": "Добавьте ноды в настройках на вкладке аддона. Аддон проверяет их каждую минуту, считает аптайм и публикует публичную страницу статуса.",
			"en": "Add your nodes in the addon tab settings. The addon probes them every minute, computes uptime and publishes a public status page.",
		},
	})
}
