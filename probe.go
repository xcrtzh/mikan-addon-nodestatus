package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

const (
	checkUp       = byte('1')
	checkDegraded = byte('2')
	checkDown     = byte('0')
	dayKeyTTL     = int64(95 * 86400) // keep ~95 days of history
	probeTimeout  = 5 * time.Second
)

// proberLoop probes every node on every tick and records the results.
func proberLoop(kv *KV) {
	// Small delay so /v2/info answers first during install checks.
	time.Sleep(3 * time.Second)
	var lastPush time.Time
	for {
		cfg := loadConfig(kv)
		if len(cfg.Nodes) > 0 {
			runProbeCycle(kv, cfg)
			if pagesConfigured(cfg) && time.Since(lastPush) >= time.Duration(cfg.PagesEveryMin)*time.Minute {
				if err := pushStatusPage(kv, cfg); err != nil {
					log.Printf("pages push: %v", err)
				} else {
					lastPush = time.Now()
				}
			}
		}
		interval := cfg.IntervalSec
		if interval < 10 {
			interval = 60
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func runProbeCycle(kv *KV, cfg Config) {
	now := time.Now()
	doc := loadStatus(kv)
	prev := map[string]string{}
	for _, n := range doc.Nodes {
		prev[n.ID] = n.Status
	}
	var out []NodeStatus
	for _, n := range cfg.Nodes {
		id := nodeID(n)
		status, latency := probeNode(n, cfg.LatencyWarnMs)
		ch := checkUp
		switch status {
		case "degraded":
			ch = checkDegraded
		case "down":
			ch = checkDown
		}
		recordCheck(kv, id, ch, now)

		st := NodeStatus{
			ID: id, Name: n.Name, Code: n.Code,
			Status: status, LatencyMs: latency,
			LastCheck: now.Unix(),
		}
		if p, ok := prev[id]; ok && p == status {
			for _, o := range doc.Nodes {
				if o.ID == id {
					st.LastChange = o.LastChange
					break
				}
			}
		} else {
			st.LastChange = now.Unix()
		}
		out = append(out, st)

		if p, ok := prev[id]; ok && p != status && p != "" && p != "unknown" {
			log.Printf("node %s: %s -> %s", n.Name, p, status)
			sendAlert(cfg, n, p, status)
		}
	}
	doc = StatusDoc{Nodes: out, UpdatedAt: now.Unix()}
	if data, err := json.Marshal(doc); err == nil {
		_ = kv.Put("status/current", data, 0)
	}
}

// probeNode dials each port and returns the best result.
func probeNode(n Node, warnMs int) (string, int64) {
	var best int64 = -1
	for _, p := range n.Ports {
		addr := net.JoinHostPort(n.Host, fmt.Sprintf("%d", p))
		t0 := time.Now()
		c, err := net.DialTimeout("tcp", addr, probeTimeout)
		if err != nil {
			continue
		}
		c.Close()
		ms := time.Since(t0).Milliseconds()
		if best == -1 || ms < best {
			best = ms
		}
	}
	if best == -1 {
		return "down", 0
	}
	if warnMs > 0 && best > int64(warnMs) {
		return "degraded", best
	}
	return "up", best
}

// recordCheck appends one character to today's per-node history key.
func recordCheck(kv *KV, id string, ch byte, now time.Time) {
	key := "day/" + id + "/" + now.UTC().Format("2006-01-02")
	cur, err := kv.Get(key)
	if err != nil {
		log.Printf("kv get %s: %v", key, err)
		return
	}
	cur = append(cur, ch)
	if err := kv.Put(key, cur, dayKeyTTL); err != nil {
		log.Printf("kv put %s: %v", key, err)
	}
}

// dayUptime returns the share of up/degraded checks for the last `days` days.
func dayUptime(kv *KV, id string, days int) float64 {
	var good, total int
	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		key := "day/" + id + "/" + now.AddDate(0, 0, -d).Format("2006-01-02")
		data, err := kv.Get(key)
		if err != nil || len(data) == 0 {
			continue
		}
		for _, ch := range data {
			total++
			if ch == checkUp || ch == checkDegraded {
				good++
			}
		}
	}
	if total == 0 {
		return -1
	}
	return float64(good) / float64(total) * 100
}

// dayBars returns per-day uptime for the last `days` days, oldest first; -1 = no data.
func dayBars(kv *KV, id string, days int) []float64 {
	out := make([]float64, days)
	now := time.Now().UTC()
	for d := 0; d < days; d++ {
		date := now.AddDate(0, 0, -(days - 1 - d))
		key := "day/" + id + "/" + date.Format("2006-01-02")
		data, err := kv.Get(key)
		if err != nil || len(data) == 0 {
			out[d] = -1
			continue
		}
		var good, total int
		for _, ch := range data {
			total++
			if ch == checkUp || ch == checkDegraded {
				good++
			}
		}
		if total == 0 {
			out[d] = -1
		} else {
			out[d] = float64(good) / float64(total) * 100
		}
	}
	return out
}

func pagesConfigured(cfg Config) bool {
	return cfg.PagesOwner != "" && cfg.PagesRepo != "" && cfg.PagesToken != ""
}

// pushStatusPage renders the public page and pushes it to GitHub Pages.
func pushStatusPage(kv *KV, cfg Config) error {
	doc := loadStatus(kv)
	page := renderPublicPage(cfg, kv, doc)
	return githubPutFile(cfg, "index.html", page,
		"status: update "+time.Now().UTC().Format("2006-01-02 15:04"))
}

func githubPutFile(cfg Config, path, content, message string) error {
	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		cfg.PagesOwner, cfg.PagesRepo, path, cfg.PagesBranch)
	req, err := http.NewRequest("GET", api, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PagesToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var cur struct {
		SHA string `json:"sha"`
	}
	sha := ""
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&cur); err == nil {
			sha = cur.SHA
		}
	} else if resp.StatusCode != 404 {
		return fmt.Errorf("github get %s: http %d", path, resp.StatusCode)
	}

	body, _ := json.Marshal(map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"sha":     sha,
		"branch":  cfg.PagesBranch,
	})
	putReq, err := http.NewRequest("PUT", api, bytes.NewReader(body))
	if err != nil {
		return err
	}
	putReq.Header.Set("Authorization", "Bearer "+cfg.PagesToken)
	putReq.Header.Set("Accept", "application/vnd.github+json")
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != 200 && putResp.StatusCode != 201 {
		return fmt.Errorf("github put %s: http %d", path, putResp.StatusCode)
	}
	return nil
}

// sendAlert emails the admin on up/down transitions when SMTP is configured.
func sendAlert(cfg Config, n Node, from, to string) {
	s := cfg.SMTP
	if s.Host == "" || s.To == "" {
		return
	}
	port := s.Port
	if port == 0 {
		port = 587
	}
	fromAddr := s.From
	if fromAddr == "" {
		fromAddr = s.User
	}
	subj := fmt.Sprintf("[%s] %s: %s -> %s", cfg.PublicTitle, n.Name, from, to)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\nNode %s (%s) changed status: %s -> %s at %s.\r\n",
		fromAddr, s.To, subj, n.Name, n.Host, from, to, time.Now().Format("2006-01-02 15:04:05"))
	addr := fmt.Sprintf("%s:%d", s.Host, port)
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	if err := smtp.SendMail(addr, auth, fromAddr, strings.Split(s.To, ","), []byte(msg)); err != nil {
		log.Printf("smtp alert: %v", err)
	}
}
