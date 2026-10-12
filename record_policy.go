package main

import (
	"fmt"
	"strings"
)

type RecordIntent struct {
	Domain  string `json:"domain"`
	Kind    string `json:"kind"`
	Slug    string `json:"slug"`
	Target  string `json:"target"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied bool   `json:"proxied,omitempty"`
}

type RecordChange struct {
	RecordType string `json:"record_type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl,omitempty"`
	Proxied    bool   `json:"proxied,omitempty"`
}

func planRecord(in RecordIntent) (RecordChange, error) {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	target := strings.TrimSpace(in.Target)
	if strings.TrimSpace(in.Domain) == "" || slug == "" || target == "" {
		return RecordChange{}, fmt.Errorf("domain, slug, and target are required")
	}
	if strings.ContainsAny(slug, " ./") {
		return RecordChange{}, fmt.Errorf("slug must be a single DNS label")
	}
	ttl := in.TTL
	if ttl == 0 {
		ttl = 300
	}
	switch in.Kind {
	case "player_asset":
		return RecordChange{RecordType: "CNAME", Name: "assets-" + slug, Content: target, TTL: ttl, Proxied: in.Proxied}, nil
	case "live_event":
		return RecordChange{RecordType: "A", Name: "live-" + slug, Content: target, TTL: ttl, Proxied: in.Proxied}, nil
	case "moderation_queue":
		return RecordChange{RecordType: "TXT", Name: "moderation-" + slug, Content: target, TTL: ttl}, nil
	default:
		return RecordChange{}, fmt.Errorf("kind must be player_asset, live_event, or moderation_queue")
	}
}
