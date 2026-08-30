package jot

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
)

// The graph is laid out on the server as plain SVG: topics are spread around a
// ring and their pages orbit each topic's centre. That is deterministic, needs
// no client-side physics, and keeps the page inside the same strict CSP as the
// rest of the site.

type graphNode struct {
	id     string
	title  string
	topic  string
	x, y   float64
	radius float64
	degree int
}

const (
	graphWidth   = 1120.0
	graphHeight  = 820.0
	graphPadding = 60.0
)

func layoutGraph(snap *siteSnapshot) ([]graphNode, [][2]int) {
	nodes := make([]graphNode, 0, len(snap.docs))
	index := make(map[string]int, len(snap.docs))

	topics := append([]string(nil), snap.topics...)
	sort.Strings(topics)

	cx, cy := graphWidth/2, graphHeight/2
	ringRadius := math.Min(graphWidth, graphHeight)/2 - graphPadding - 90
	if len(topics) <= 1 {
		ringRadius = 0
	}

	for ti, topic := range topics {
		pages := snap.byTopic[topic]
		angle := 2 * math.Pi * float64(ti) / math.Max(1, float64(len(topics)))
		tx := cx + ringRadius*math.Cos(angle)
		ty := cy + ringRadius*math.Sin(angle)

		// Orbit radius grows with the page count so dense topics stay legible.
		orbit := 26 + 11*math.Sqrt(float64(len(pages)))
		if len(pages) == 1 {
			orbit = 0
		}
		for pi, d := range pages {
			pa := 2 * math.Pi * float64(pi) / math.Max(1, float64(len(pages)))
			degree := len(snap.graph.Backlinks(d.ID))
			for _, l := range snap.graph.Out[d.ID] {
				if l.TargetID != "" {
					degree++
				}
			}
			index[d.ID] = len(nodes)
			nodes = append(nodes, graphNode{
				id: d.ID, title: d.Title, topic: topic,
				x: tx + orbit*math.Cos(pa), y: ty + orbit*math.Sin(pa),
				radius: 4 + math.Min(9, math.Sqrt(float64(degree))*2.2),
				degree: degree,
			})
		}
	}

	seen := map[[2]int]bool{}
	var edges [][2]int
	for _, d := range snap.docs {
		from, ok := index[d.ID]
		if !ok {
			continue
		}
		for _, l := range snap.graph.Out[d.ID] {
			to, ok := index[l.TargetID]
			if !ok || to == from {
				continue
			}
			key := [2]int{from, to}
			if from > to {
				key = [2]int{to, from}
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, key)
		}
	}
	return nodes, edges
}

// renderGraph emits the SVG. Every node is a link, so the graph doubles as a
// navigation surface rather than only a picture.
func renderGraph(snap *siteSnapshot) string {
	nodes, edges := layoutGraph(snap)
	if len(nodes) == 0 {
		return `<p class="empty">No compiled pages to graph yet.</p>`
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<div class="graphwrap"><svg class="graph" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img" aria-label="Wiki link graph">`,
		graphWidth, graphHeight, graphWidth, graphHeight)

	for _, e := range edges {
		a, b := nodes[e[0]], nodes[e[1]]
		fmt.Fprintf(&out, `<line class="edge" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, a.x, a.y, b.x, b.y)
	}

	// Topic labels sit just outside each cluster.
	placed := map[string]bool{}
	for _, n := range nodes {
		if placed[n.topic] {
			continue
		}
		placed[n.topic] = true
		var sumX, sumY, count float64
		var maxR float64
		for _, m := range nodes {
			if m.topic == n.topic {
				sumX += m.x
				sumY += m.y
				count++
			}
		}
		tx, ty := sumX/count, sumY/count
		for _, m := range nodes {
			if m.topic == n.topic {
				if d := math.Hypot(m.x-tx, m.y-ty); d > maxR {
					maxR = d
				}
			}
		}
		fmt.Fprintf(&out, `<circle class="ring" cx="%.1f" cy="%.1f" r="%.1f"/>`, tx, ty, maxR+18)
		fmt.Fprintf(&out, `<text class="topic" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`,
			tx, ty-maxR-26, html.EscapeString(topicLabel(n.topic)))
	}

	for _, n := range nodes {
		fmt.Fprintf(&out, `<a class="node" href="/wiki/%s"><title>%s (%d links)</title><circle cx="%.1f" cy="%.1f" r="%.1f"/><text x="%.1f" y="%.1f" text-anchor="middle">%s</text></a>`,
			pathURL(n.id), html.EscapeString(n.title), n.degree,
			n.x, n.y, n.radius,
			n.x, n.y-n.radius-4, html.EscapeString(truncateLabel(n.title, 22)))
	}
	out.WriteString(`</svg></div>`)
	return out.String()
}

func truncateLabel(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}
