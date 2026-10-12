package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestPlanRecord(t *testing.T) {
	tests := []struct {
		name string
		in   RecordIntent
		want RecordChange
	}{
		{"player asset", RecordIntent{Domain: "game.example", Kind: "player_asset", Slug: "Ada", Target: "cdn.example", Proxied: true}, RecordChange{RecordType: "CNAME", Name: "assets-ada", Content: "cdn.example", TTL: 300, Proxied: true}},
		{"live event", RecordIntent{Domain: "game.example", Kind: "live_event", Slug: "Final", Target: "192.0.2.8", TTL: 30}, RecordChange{RecordType: "A", Name: "live-final", Content: "192.0.2.8", TTL: 30}},
		{"moderation queue", RecordIntent{Domain: "game.example", Kind: "moderation_queue", Slug: "EU", Target: "active"}, RecordChange{RecordType: "TXT", Name: "moderation-eu", Content: "active", TTL: 300}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planRecord(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("planRecord() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestClientResolvesZoneBeforeUpsertAndRetries429(t *testing.T) {
	var paths []string
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/dns/domain/get":
			if r.URL.Query().Get("domain") != "game.example" {
				t.Fatal("domain query missing")
			}
			fmt.Fprint(w, `{"ok":true,"data":{"zone_id":"zone-7"},"metadata":{}}`)
		case "/v1/dns/record/upsert":
			attempts++
			var body struct {
				ZoneID string `json:"zone_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ZoneID != "zone-7" {
				t.Fatalf("upsert zone_id = %q, decode error = %v", body.ZoneID, err)
			}
			if r.Header.Get("Idempotency-Key") == "" {
				t.Fatal("idempotency key missing")
			}
			if attempts == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"ok":false,"error":{"code":"busy","message":"retry later"},"metadata":{}}`)
				return
			}
			fmt.Fprint(w, `{"ok":true,"data":{"record_id":"record-9"},"metadata":{}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newDNSClient(t.Name())
	client.baseURL = server.URL
	client.sleep = func(context.Context, time.Duration) error { return nil }
	zoneID, err := client.zoneID(context.Background(), "game.example")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.upsert(context.Background(), zoneID, RecordChange{RecordType: "A", Name: "live-final", Content: "192.0.2.8", TTL: 30}, "stable-operation")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /v1/dns/domain/get", "PUT /v1/dns/record/upsert", "PUT /v1/dns/record/upsert"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("request order = %v, want %v", paths, want)
	}
}
