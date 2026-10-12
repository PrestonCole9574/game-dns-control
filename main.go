package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type adminServer struct {
	dns *DNSClient
}

func main() {
	key := strings.TrimSpace(os.Getenv("INFRAI_API_KEY"))
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	server := adminServer{dns: newDNSClient(key)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/dns/records", server.applyRecord)
	mux.HandleFunc("GET /admin/dns/records", server.listRecords)

	httpServer := &http.Server{
		Addr:              envOr("ADDR", ":8080"),
		Handler:           requestLog(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("game DNS admin listening on %s", httpServer.Addr)
	log.Fatal(httpServer.ListenAndServe())
}

func (s adminServer) applyRecord(w http.ResponseWriter, r *http.Request) {
	var intent RecordIntent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON input"})
		return
	}
	change, err := planRecord(intent)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	zoneID, err := s.dns.zoneID(r.Context(), intent.Domain)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	result, err := s.dns.upsert(r.Context(), zoneID, change, operationKey(intent.Domain, change))
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"zone_id": zoneID, "record": change, "result": result})
}

func (s adminServer) listRecords(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}
	zoneID, err := s.dns.zoneID(r.Context(), domain)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	records, err := s.dns.list(r.Context(), zoneID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"zone_id": zoneID, "records": records})
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		status = apiErr.HTTPStatus
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("method=%s path=%s duration_ms=%d", r.Method, r.URL.Path, time.Since(started).Milliseconds())
	})
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
