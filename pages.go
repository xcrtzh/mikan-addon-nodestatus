package main

import (
	"fmt"
	"strings"
	"time"
)

type pageNode struct {
	Name     string
	Code     string
	Status   string
	Latency  int64
	Uptime24 string
	Uptime7  string
	Uptime30 string
	Bars     string // 90 divs
}

func pct(v float64) string {
	if v < 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", v)
}

func barColor(v float64) string {
	if v < 0 {
		return "#3f3f46"
	}
	switch {
	case v >= 99:
		return "#22c55e"
	case v >= 95:
		return "#eab308"
	default:
		return "#ef4444"
	}
}

// renderPublicPage builds the static status page pushed to GitHub Pages.
func renderPublicPage(cfg Config, kv *KV, doc StatusDoc) string {
	title := cfg.PublicTitle
	if title == "" {
		title = "Статус серверов"
	}
	overall := "up"
	overallText := "Все системы работают"
	overallColor := "#22c55e"
	for _, n := range doc.Nodes {
		if n.Status == "down" {
			overall, overallText, overallColor = "down", "Есть проблемы", "#ef4444"
			break
		}
		if n.Status == "degraded" && overall == "up" {
			overall, overallText, overallColor = "degraded", "Частичные проблемы", "#eab308"
		}
	}
	_ = overall

	var nodes strings.Builder
	for _, ns := range doc.Nodes {
		bars := dayBars(kv, ns.ID, 90)
		var bb strings.Builder
		for _, v := range bars {
			bb.WriteString(fmt.Sprintf(`<div class="bar" style="background:%s" title="%.1f%%"></div>`, barColor(v), v))
		}
		lat := fmt.Sprintf("%d мс", ns.LatencyMs)
		if ns.Status == "down" {
			lat = "—"
		}
		dot := "#22c55e"
		if ns.Status == "degraded" {
			dot = "#eab308"
		} else if ns.Status == "down" {
			dot = "#ef4444"
		}
		fmt.Fprintf(&nodes, `
    <div class="node">
      <div class="node-head">
        <span class="dot" style="background:%s"></span>
        <span class="node-name">%s <span class="node-code">%s</span></span>
        <span class="lat">%s</span>
      </div>
      <div class="bars">%s</div>
      <div class="meta"><span>24 ч · <b>%s</b></span><span>7 дн · <b>%s</b></span><span>30 дн · <b>%s</b></span></div>
    </div>`,
			dot, esc(ns.Name), esc(ns.Code), lat, bb.String(),
			pct(dayUptime(kv, ns.ID, 1)), pct(dayUptime(kv, ns.ID, 7)), pct(dayUptime(kv, ns.ID, 30)))
	}
	if nodes.Len() == 0 {
		nodes.WriteString(`<div class="node"><div class="node-head"><span class="node-name">Ноды пока не добавлены</span></div></div>`)
	}

	updated := time.Now().Format("02.01.2006 15:04")
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="120">
<title>%s</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f1115;color:#e7e9ee;min-height:100vh;-webkit-text-size-adjust:100%%}
.wrap{max-width:680px;margin:0 auto;padding:20px 16px}
.header{display:flex;align-items:center;justify-content:space-between;gap:12px;background:#171a21;border:1px solid #262b36;border-radius:14px;padding:16px 18px;margin-bottom:12px}
.header h1{font-size:18px;font-weight:700;line-height:1.3}
.overall{display:flex;align-items:center;gap:8px;font-size:13px;color:#9aa1ad;white-space:nowrap}
.dot{width:10px;height:10px;border-radius:50%%;display:inline-block;flex-shrink:0}
.node{background:#171a21;border:1px solid #262b36;border-radius:14px;padding:16px 18px;margin-bottom:12px}
.node-head{display:flex;align-items:center;gap:10px;margin-bottom:12px;min-height:28px}
.node-name{font-weight:600;font-size:15px;line-height:1.3}
.node-code{color:#9aa1ad;font-weight:400;font-size:13px}
.lat{margin-left:auto;font-size:13px;color:#9aa1ad;white-space:nowrap;font-variant-numeric:tabular-nums}
.bars{display:flex;gap:2px;margin-bottom:10px;min-height:26px}
.bar{flex:1;min-width:2px;height:26px;border-radius:2px}
.meta{display:flex;gap:16px;flex-wrap:wrap;font-size:12px;color:#9aa1ad}
.meta b{color:#e7e9ee;font-variant-numeric:tabular-nums}
.footer{text-align:center;color:#6b7280;font-size:12px;margin-top:16px;padding:0 8px;line-height:1.5}
@media (max-width:480px){
.wrap{padding:12px 10px}
.header{padding:14px 14px;border-radius:12px}
.header h1{font-size:16px}
.node{padding:14px;border-radius:12px}
.node-head{flex-wrap:wrap;row-gap:6px}
.lat{font-size:12px}
.bars{overflow-x:auto;padding-bottom:4px;-webkit-overflow-scrolling:touch}
.bar{min-width:3px;height:30px}
.meta{gap:12px;font-size:11px}
}
</style>
</head>
<body>
<div class="wrap">
  <div class="header"><h1>%s</h1><div class="overall"><span class="dot" style="background:%s"></span>%s</div></div>
  %s
  <div class="footer">Обновлено %s · автообновление каждые 2 мин</div>
</div>
</body>
</html>`, esc(title), esc(title), overallColor, overallText, nodes.String(), updated)
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
