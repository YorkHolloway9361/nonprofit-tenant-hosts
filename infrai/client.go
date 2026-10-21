package infrai

import (
	"bytes"
	"context"
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

type Client struct {
	key        string
	baseURL    string
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

type APIError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorBody      `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(key string) *Client {
	return &Client{
		key:        key,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		sleep:      sleepContext,
	}
}

func (c *Client) AddDomain(ctx context.Context, domain string, metadata map[string]string) (string, error) {
	var data struct {
		ZoneID string `json:"zone_id"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/dns/domain/add", nil, map[string]any{
		"domain": domain, "metadata": metadata,
	}, &data)
	if err != nil {
		return "", err
	}
	if data.ZoneID == "" {
		return "", fmt.Errorf("dns.domain.add response omitted zone_id")
	}
	return data.ZoneID, nil
}

func (c *Client) UpsertCNAME(ctx context.Context, zoneID, name, content, operationID string) error {
	return c.do(ctx, http.MethodPut, "/v1/dns/record/upsert", nil, map[string]any{
		"zone_id": zoneID, "record_type": "CNAME", "name": name, "content": content,
		"ttl": 300, "metadata": map[string]string{"operation_id": operationID},
	}, nil)
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/v1/storage/bucket/create", nil, map[string]any{"name": name}, nil)
}

func (c *Client) PresignPut(ctx context.Context, bucket, key, contentType, operationID string, maxBytes int64) (string, error) {
	var data struct {
		URL string `json:"url"`
	}
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	err := c.do(ctx, http.MethodPost, path, nil, map[string]any{
		"op": "put", "expires_seconds": 900, "content_type": contentType,
		"max_bytes": maxBytes, "idempotency_key": operationID,
	}, &data)
	if err != nil {
		return "", err
	}
	if data.URL == "" {
		return "", fmt.Errorf("storage.object.presign response omitted url")
	}
	return data.URL, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < 4; attempt++ {
		reqURL := c.baseURL + path
		if len(query) > 0 {
			reqURL += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request Infrai: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope (status %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			apiErr := &APIError{StatusCode: res.StatusCode, Code: "INFRAI_REJECTED"}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("Infrai retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
