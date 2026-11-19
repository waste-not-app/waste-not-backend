package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/waste-not-app/waste-not-backend/contracts"
	"github.com/waste-not-app/waste-not-backend/internal/config"
)

type Error struct {
	Status     int
	Code       string
	RetryAfter int
}

func (e *Error) Error() string { return e.Code }

var ErrInvalid = &Error{503, "INVALID_AI_RESPONSE", 0}

type Client struct {
	config       config.Config
	http         *http.Client
	schemas      map[string]*jsonschema.Schema
	documents    map[string]map[string]any
	catalogMu    sync.Mutex
	catalogCache atomic.Pointer[catalogSnapshot]
}

func New(c config.Config) (*Client, error) {
	client := &Client{config: c, http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 23 * time.Second, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second}}, schemas: map[string]*jsonschema.Schema{}, documents: map[string]map[string]any{}}
	specs := map[string][]string{"public.openapi.json": {"RecommendationInput", "DetectionSession", "RecommendationSession", "ObjectCatalog"}, "private-detection.openapi.json": {"InternalDetection", "DetectionCapabilities"}, "private-recommendation.openapi.json": {"RecommendationResult"}}
	for name, models := range specs {
		raw, e := contracts.Files.ReadFile("ai/" + name)
		if e != nil {
			return nil, e
		}
		var doc map[string]any
		if e = json.Unmarshal(raw, &doc); e != nil {
			return nil, e
		}
		client.documents[name] = doc
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		uri := "urn:wastenot:" + name
		if e = compiler.AddResource(uri, doc); e != nil {
			return nil, e
		}
		for _, model := range models {
			schema, e := compiler.Compile(uri + "#/components/schemas/" + model)
			if e != nil {
				return nil, fmt.Errorf("AI schema %s: %w", model, e)
			}
			client.schemas[model] = schema
		}
	}
	return client, nil
}
func (c *Client) Validate(name string, v any) error {
	schema := c.schemas[name]
	if schema == nil {
		return ErrInvalid
	}
	if schema.Validate(v) != nil {
		return ErrInvalid
	}
	return nil
}
func (c *Client) NormalizeInput(v map[string]any) error {
	if err := c.Validate("RecommendationInput", v); err != nil {
		return err
	}
	doc := c.documents["public.openapi.json"]
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	applyDefaults(v, schemas["RecommendationInput"].(map[string]any), schemas)
	return c.Validate("RecommendationInput", v)
}
func applyDefaults(v map[string]any, schema, schemas map[string]any) {
	if ref, ok := schema["$ref"].(string); ok {
		for name, definition := range schemas {
			if ref == "#/components/schemas/"+name {
				schema = definition.(map[string]any)
				break
			}
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for key, value := range properties {
		prop := value.(map[string]any)
		if _, ok := v[key]; !ok {
			if def, exists := prop["default"]; exists {
				raw, _ := json.Marshal(def)
				var copy any
				_ = json.Unmarshal(raw, &copy)
				v[key] = copy
			}
		}
		if object, ok := v[key].(map[string]any); ok {
			applyDefaults(object, prop, schemas)
		}
	}
}
func (c *Client) rpc(ctx context.Context, requestID, origin, path, token, mime string, body []byte) (map[string]any, error) {
	method := "POST"
	if body == nil {
		method = "GET"
	}
	req, e := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(body))
	if e != nil {
		return nil, &Error{503, "AI_UNAVAILABLE", 0}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", requestID)
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	resp, e := c.http.Do(req)
	if e != nil {
		var n net.Error
		if errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &n) && n.Timeout()) {
			return nil, &Error{504, "AI_TIMEOUT", 0}
		}
		return nil, &Error{503, "AI_UNAVAILABLE", 0}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if e != nil || len(raw) > 2*1024*1024 {
		return nil, ErrInvalid
	}
	var envelope struct {
		APIVersion string         `json:"api_version"`
		Data       map[string]any `json:"data"`
		Error      struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, ErrInvalid
	}
	if resp.StatusCode != http.StatusOK {
		status := resp.StatusCode
		code := envelope.Error.Code
		allowed := map[string]int{"AI_BUSY": 503, "AI_UNAVAILABLE": 503, "INVALID_AI_RESPONSE": 503, "AI_TIMEOUT": 504, "INVALID_CONTEXT": 422, "INVALID_AI_REFERENCE": 422, "OBJECT_REQUIRES_MANUAL": 422, "VALIDATION_ERROR": 422, "INVALID_IMAGE": 422, "IMAGE_DIMENSIONS_EXCEEDED": 422, "FILE_TOO_LARGE": 413, "UNSUPPORTED_MEDIA_TYPE": 415, "RATE_LIMITED": 429}
		if allowed[code] != status {
			return nil, ErrInvalid
		}
		retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if retry < 0 || retry > 86400 {
			retry = 0
		}
		return nil, &Error{status, code, retry}
	}
	if envelope.APIVersion != "1.0.0" || envelope.Data == nil {
		return nil, ErrInvalid
	}
	return envelope.Data, nil
}

type catalogSnapshot struct {
	value map[string]any
	until time.Time
}

func (c *Client) Catalog(ctx context.Context, id string) (map[string]any, error) {
	cached := c.catalogCache.Load()
	if cached != nil && time.Now().Before(cached.until) {
		return cached.value, nil
	}
	if !c.catalogMu.TryLock() {
		if cached != nil {
			return cached.value, nil
		}
		return nil, &Error{503, "AI_BUSY", 1}
	}
	defer c.catalogMu.Unlock()
	if cached := c.catalogCache.Load(); cached != nil && time.Now().Before(cached.until) {
		return cached.value, nil
	}

	catalog, e := c.rpc(ctx, id, c.config.RecommendationURL, "/internal/v1/objects", c.config.RecommendationToken, "", nil)
	if e != nil {
		return nil, e
	}
	if e = c.Validate("ObjectCatalog", catalog); e != nil {
		return nil, e
	}
	cap, e := c.rpc(ctx, id, c.config.DetectionURL, "/internal/v1/capabilities", c.config.DetectionToken, "", nil)
	if e != nil {
		return nil, e
	}
	if e = c.Validate("DetectionCapabilities", cap); e != nil {
		return nil, e
	}
	for _, key := range []string{"mapping_version", "supported_visual_classes", "supported_object_labels", "review_profile", "class_thresholds", "review_only_classes"} {
		if v, ok := cap[key]; ok {
			catalog[key] = v
		}
	}
	model, ok := cap["model"].(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	catalog["model_version"] = model["model_version"]
	if e = c.Validate("ObjectCatalog", catalog); e != nil {
		return nil, e
	}
	c.catalogCache.Store(&catalogSnapshot{catalog, time.Now().Add(30 * time.Second)})
	return catalog, nil
}
func (c *Client) Detect(ctx context.Context, id, mime string, body []byte) (map[string]any, error) {
	v, e := c.rpc(ctx, id, c.config.DetectionURL, "/internal/v1/detect", c.config.DetectionToken, mime, body)
	if e != nil {
		return nil, e
	}
	if e = c.Validate("InternalDetection", v); e != nil {
		return nil, e
	}
	if !validDetection(v) {
		return nil, ErrInvalid
	}
	return v, nil
}
func (c *Client) Recommend(ctx context.Context, id string, body any) (map[string]any, error) {
	raw, _ := json.Marshal(body)
	v, e := c.rpc(ctx, id, c.config.RecommendationURL, "/internal/v1/recommendations", c.config.RecommendationToken, "application/json", raw)
	if e != nil {
		return nil, e
	}
	if e = c.Validate("RecommendationResult", v); e != nil {
		return nil, e
	}
	if !c.config.AllowPrototype && (v["prototype_only"] == true || v["provider"] == "fixture") {
		return nil, &Error{503, "AI_UNAVAILABLE", 0}
	}
	return v, nil
}
