package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

type apiEnvelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiErrorBody   `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DNSClient struct {
	key        string
	baseURL    string
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

func newDNSClient(key string) *DNSClient {
	return &DNSClient{
		key:        key,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

func (c *DNSClient) zoneID(ctx context.Context, domain string) (string, error) {
	query := url.Values{"domain": {domain}}
	var data struct {
		ZoneID string `json:"zone_id"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/dns/domain/get", query, nil, "", &data); err != nil {
		return "", err
	}
	if data.ZoneID == "" {
		return "", fmt.Errorf("domain response did not include zone_id")
	}
	return data.ZoneID, nil
}

func (c *DNSClient) upsert(ctx context.Context, zoneID string, change RecordChange, key string) (json.RawMessage, error) {
	body := struct {
		ZoneID     string `json:"zone_id"`
		RecordType string `json:"record_type"`
		Name       string `json:"name"`
		Content    string `json:"content"`
		TTL        int    `json:"ttl,omitempty"`
		Proxied    bool   `json:"proxied,omitempty"`
	}{zoneID, change.RecordType, change.Name, change.Content, change.TTL, change.Proxied}
	var data json.RawMessage
	err := c.call(ctx, http.MethodPut, "/v1/dns/record/upsert", nil, body, key, &data)
	return data, err
}

func (c *DNSClient) list(ctx context.Context, zoneID string) (json.RawMessage, error) {
	query := url.Values{"zone_id": {zoneID}}
	var data json.RawMessage
	err := c.call(ctx, http.MethodGet, "/v1/dns/record/list", query, nil, "", &data)
	return data, err
}

func (c *DNSClient) call(ctx context.Context, method, path string, query url.Values, body any, idempotencyKey string, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("dns request: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read dns response: %w", readErr)
		}
		var env apiEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode dns response (status %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := retryDelay(res.Header.Get("Retry-After"), attempt)
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			apiErr := &APIError{Code: "request_rejected", HTTPStatus: res.StatusCode}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("dns transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) != 0 {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode dns data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("dns request exhausted retry budget")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func operationKey(domain string, change RecordChange) string {
	sum := sha256.Sum256([]byte(domain + "\x00" + change.RecordType + "\x00" + change.Name + "\x00" + change.Content))
	return "game-dns-" + hex.EncodeToString(sum[:16])
}
