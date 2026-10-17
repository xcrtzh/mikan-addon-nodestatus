package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

//go:embed ui/index.html
var adminHTML string

//go:embed ui/style.css
var adminCSS string

func handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"code": "invalid", "message": "method not allowed"})
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/ui/") {
	case "index.html", "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(adminHTML))
	case "style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write([]byte(adminCSS))
	default:
		writeJSON(w, 404, map[string]string{"code": "not_found", "message": "no such file"})
	}
}

type rpcCall struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

func handleRPC(kv *KV) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"code": "invalid", "message": "method not allowed"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, 400, map[string]string{"code": "invalid", "message": "bad body"})
			return
		}
		var c rpcCall
		if err := json.Unmarshal(body, &c); err != nil {
			writeJSON(w, 400, map[string]string{"code": "invalid", "message": "bad json"})
			return
		}
		switch c.Method + " " + c.Path {
		case "GET /status":
			serveStatus(w, kv)
		case "GET /config":
			writeJSON(w, 200, loadConfig(kv).sanitized())
		case "POST /config":
			serveSaveConfig(w, kv, c.Body)
		case "GET /history":
			serveHistory(w, kv, r)
		default:
			writeJSON(w, 404, map[string]string{"code": "not_found", "message": "unknown rpc"})
		}
	}
}

func serveSaveConfig(w http.ResponseWriter, kv *KV, raw json.RawMessage) {
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		writeJSON(w, 400, map[string]string{"code": "invalid", "message": "bad config json"})
		return
	}
	// Keep existing secrets when the form sends empty ones.
	old := loadConfig(kv)
	if c.PagesToken == "" {
		c.PagesToken = old.PagesToken
	}
	if c.SMTP.Pass == "" {
		c.SMTP.Pass = old.SMTP.Pass
	}
	if len(c.Nodes) > 16 {
		writeJSON(w, 400, map[string]string{"code": "invalid", "message": "too many nodes (max 16)"})
		return
	}
	for i, n := range c.Nodes {
		if strings.TrimSpace(n.Host) == "" {
			writeJSON(w, 400, map[string]string{"code": "invalid", "message": fmt.Sprintf("node %d: empty host", i+1)})
			return
		}
		if len(n.Ports) == 0 {
			writeJSON(w, 400, map[string]string{"code": "invalid", "message": fmt.Sprintf("node %d: no ports", i+1)})
			return
		}
	}
	data, _ := json.Marshal(c)
	if err := kv.Put("config", data, 0); err != nil {
		writeJSON(w, 500, map[string]string{"code": "invalid", "message": "kv error"})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type nodeView struct {
	NodeStatus
	Uptime24 float64   `json:"uptime_24"`
	Uptime7  float64   `json:"uptime_7"`
	Uptime30 float64   `json:"uptime_30"`
	Bars     []float64 `json:"bars"`
}

func serveStatus(w http.ResponseWriter, kv *KV) {
	cfg := loadConfig(kv)
	doc := loadStatus(kv)
	views := make([]nodeView, 0, len(doc.Nodes))
	byID := map[string]Node{}
	for _, n := range cfg.Nodes {
		byID[nodeID(n)] = n
	}
	for _, ns := range doc.Nodes {
		views = append(views, nodeView{
			NodeStatus: ns,
			Uptime24:   dayUptime(kv, ns.ID, 1),
			Uptime7:    dayUptime(kv, ns.ID, 7),
			Uptime30:   dayUptime(kv, ns.ID, 30),
			Bars:       dayBars(kv, ns.ID, 90),
		})
	}
	// Nodes in config without any probe yet.
	for _, n := range cfg.Nodes {
		id := nodeID(n)
		found := false
		for _, v := range views {
			if v.ID == id {
				found = true
				break
			}
		}
		if !found {
			views = append(views, nodeView{
				NodeStatus: NodeStatus{ID: id, Name: n.Name, Code: n.Code, Status: "unknown"},
				Uptime24:   -1, Uptime7: -1, Uptime30: -1,
			})
		}
	}
	writeJSON(w, 200, map[string]any{
		"nodes":      views,
		"updated_at": doc.UpdatedAt,
		"now":        time.Now().Unix(),
	})
}

func serveHistory(w http.ResponseWriter, kv *KV, r *http.Request) {
	// Kept for future use; the dashboard reads bars from /status.
	writeJSON(w, 200, map[string]bool{"ok": true})
}
