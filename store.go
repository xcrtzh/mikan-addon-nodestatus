package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	adapterID      = "nodestatus"
	adapterVersion = "1.0.0"
)

// KV is a minimal client for the panel KV API.
type KV struct {
	base   string
	tok    string
	client *http.Client
}

func NewKV(base, tok string) *KV {
	return &KV{base: base, tok: tok, client: &http.Client{Timeout: 20 * time.Second}}
}

func (k *KV) req(method, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequest(method, k.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+k.tok)
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}
	return data, resp.StatusCode, nil
}

// Get returns the value or nil when the key is missing.
func (k *KV) Get(key string) ([]byte, error) {
	data, code, err := k.req("GET", "/kv/"+url.PathEscape(key), nil)
	if err != nil {
		return nil, err
	}
	if code == 404 {
		return nil, nil
	}
	if code != 200 {
		return nil, fmt.Errorf("kv get %s: http %d", key, code)
	}
	return data, nil
}

// Put stores the value; ttlSec <= 0 means no expiry.
func (k *KV) Put(key string, val []byte, ttlSec int64) error {
	path := "/kv/" + url.PathEscape(key)
	if ttlSec > 0 {
		path += fmt.Sprintf("?ttl=%d", ttlSec)
	}
	_, code, err := k.req("PUT", path, val)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("kv put %s: http %d", key, code)
	}
	return nil
}

// ---- config ----

type Node struct {
	Name  string `json:"name"`
	Code  string `json:"code"`
	Host  string `json:"host"`
	Ports []int  `json:"ports"`
}

type SMTPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Pass string `json:"pass"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Config struct {
	Nodes         []Node     `json:"nodes"`
	IntervalSec   int        `json:"interval_sec"`
	LatencyWarnMs int        `json:"latency_warn_ms"`
	PublicTitle   string     `json:"public_title"`
	UITheme       string     `json:"ui_theme"`
	PagesOwner    string     `json:"pages_owner"`
	PagesRepo     string     `json:"pages_repo"`
	PagesBranch   string     `json:"pages_branch"`
	PagesToken    string     `json:"pages_token"`
	PagesEveryMin int        `json:"pages_every_min"`
	SMTP          SMTPConfig `json:"smtp"`
}

func defaultConfig() Config {
	return Config{
		IntervalSec:   60,
		LatencyWarnMs: 500,
		PublicTitle:   "Статус серверов",
		UITheme:       "system",
		PagesBranch:   "main",
		PagesEveryMin: 2,
	}
}

func nodeID(n Node) string {
	id := strings.ToLower(strings.TrimSpace(n.Code))
	if id == "" {
		id = strings.ToLower(strings.TrimSpace(n.Name))
	}
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "node"
	}
	return b.String()
}

func loadConfig(kv *KV) Config {
	cfg := defaultConfig()
	data, err := kv.Get("config")
	if err != nil || len(data) == 0 {
		return cfg
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return cfg
	}
	if c.IntervalSec < 10 {
		c.IntervalSec = 10
	}
	if c.IntervalSec > 3600 {
		c.IntervalSec = 3600
	}
	if c.PagesEveryMin < 1 {
		c.PagesEveryMin = 1
	}
	if c.PagesBranch == "" {
		c.PagesBranch = "main"
	}
	return c
}

func (c Config) sanitized() Config {
	// Never expose secrets through RPC.
	c.PagesToken = ""
	c.SMTP.Pass = ""
	return c
}

// ---- status ----

type NodeStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Status     string `json:"status"` // up | degraded | down | unknown
	LatencyMs  int64  `json:"latency_ms"`
	LastCheck  int64  `json:"last_check"`
	LastChange int64  `json:"last_change"`
}

type StatusDoc struct {
	Nodes     []NodeStatus `json:"nodes"`
	UpdatedAt int64        `json:"updated_at"`
}

func loadStatus(kv *KV) StatusDoc {
	var d StatusDoc
	data, err := kv.Get("status/current")
	if err != nil || len(data) == 0 {
		return d
	}
	_ = json.Unmarshal(data, &d)
	return d
}
