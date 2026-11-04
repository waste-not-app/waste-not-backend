package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

const maxImage = 5 * 1024 * 1024

func (s *Server) aiEnabled(w http.ResponseWriter, r *http.Request) bool {
	if !s.Config.AIEnabled {
		fail(w, r, 503, "AI_DISABLED", nil)
		return false
	}
	return true
}
func aiFailure(w http.ResponseWriter, r *http.Request, e error) {
	var a *ai.Error
	if !errors.As(e, &a) {
		a = ai.ErrInvalid
	}
	if a.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(a.RetryAfter))
	}
	fail(w, r, a.Status, a.Code, nil)
}
func (s *Server) aiObjects(w http.ResponseWriter, r *http.Request) {
	if !s.aiEnabled(w, r) {
		return
	}
	v, e := s.AI.Catalog(r.Context(), requestID(r))
	if e != nil {
		aiFailure(w, r, e)
		return
	}
	data(w, r, 200, v)
}

type operation struct {
	ID, State, Hash    string
	Response, Snapshot json.RawMessage
	Status             int
	Code               string
	RetryAfter         int
	Expires            time.Time
	Deleted            bool
}

const operationColumns = `id::text,state,request_hash,response,snapshot,COALESCE(http_status,0),COALESCE(error_code,''),COALESCE(retry_after,0),expires_at,deleted_at IS NOT NULL`

func scanOperation(row pgx.Row) (operation, error) {
	var o operation
	e := row.Scan(&o.ID, &o.State, &o.Hash, &o.Response, &o.Snapshot, &o.Status, &o.Code, &o.RetryAfter, &o.Expires, &o.Deleted)
	return o, e
}
func (s *Server) claim(w http.ResponseWriter, r *http.Request, kind, clientID, hash string, parent *string) (operation, bool) {
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.internal(w, r, e)
		return operation{}, false
	}
	defer tx.Rollback(ctx)
	expires := s.Now().UTC().Add(24 * time.Hour)
	if parent != nil {
		o, err := scanOperation(tx.QueryRow(ctx, `SELECT `+operationColumns+` FROM ai_operations WHERE id=$1 AND user_id=$2 AND operation='DETECTION' FOR UPDATE`, *parent, who(r).User.ID))
		if !s.checkOperation(w, r, o, err) {
			return operation{}, false
		}
		expires = o.Expires
	}
	id := uuid.NewString()
	tag, e := tx.Exec(ctx, `INSERT INTO ai_operations(id,user_id,operation,client_request_id,request_hash,detection_id,state,expires_at) VALUES($1,$2,$3,$4,$5,$6,'IN_PROGRESS',$7) ON CONFLICT(user_id,operation,client_request_id) DO NOTHING`, id, who(r).User.ID, kind, clientID, hash, parent, expires)
	if e != nil {
		s.internal(w, r, e)
		return operation{}, false
	}
	o, e := scanOperation(tx.QueryRow(ctx, `SELECT `+operationColumns+` FROM ai_operations WHERE user_id=$1 AND operation=$2 AND client_request_id=$3`, who(r).User.ID, kind, clientID))
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		s.internal(w, r, e)
		return operation{}, false
	}
	if !s.checkOperation(w, r, o, nil) {
		return operation{}, false
	}
	if o.Hash != hash {
		fail(w, r, 409, "IDEMPOTENCY_CONFLICT", nil)
		return operation{}, false
	}
	if tag.RowsAffected() == 0 {
		s.replay(w, r, o)
		return operation{}, false
	}
	for _, quota := range []struct {
		key    string
		n      int
		window time.Duration
	}{{"ai-minute:", 10, time.Minute}, {"ai-day:", 100, 24 * time.Hour}} {
		ok, err := s.limit(r, quota.key+who(r).User.ID, quota.n, quota.window)
		if err != nil {
			s.complete(w, r, o, nil, nil, &ai.Error{Status: 503, Code: "AI_UNAVAILABLE"})
			return operation{}, false
		}
		if !ok {
			s.complete(w, r, o, nil, nil, &ai.Error{Status: 429, Code: "RATE_LIMITED", RetryAfter: int(quota.window.Seconds())})
			return operation{}, false
		}
	}
	return o, true
}
func (s *Server) checkOperation(w http.ResponseWriter, r *http.Request, o operation, e error) bool {
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return false
	}
	if e != nil {
		s.internal(w, r, e)
		return false
	}
	if o.Deleted {
		fail(w, r, 410, "AI_SESSION_EXPIRED", nil)
		return false
	}
	if !o.Expires.After(s.Now()) {
		fail(w, r, 410, "AI_SESSION_EXPIRED", nil)
		return false
	}
	return true
}
func (s *Server) replay(w http.ResponseWriter, r *http.Request, o operation) {
	timer := time.NewTimer(27 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for o.State == "IN_PROGRESS" {
		select {
		case <-r.Context().Done():
			fail(w, r, 504, "AI_TIMEOUT", nil)
			return
		case <-timer.C:
			w.Header().Set("Retry-After", "2")
			fail(w, r, 503, "AI_BUSY", nil)
			return
		case <-ticker.C:
			var e error
			o, e = scanOperation(s.DB.QueryRow(r.Context(), `SELECT `+operationColumns+` FROM ai_operations WHERE id=$1 AND user_id=$2`, o.ID, who(r).User.ID))
			if !s.checkOperation(w, r, o, e) {
				return
			}
		}
	}
	if !s.checkOperation(w, r, o, nil) {
		return
	}
	if o.Code != "" {
		aiFailure(w, r, &ai.Error{Status: o.Status, Code: o.Code, RetryAfter: o.RetryAfter})
		return
	}
	if !json.Valid(o.Response) {
		aiFailure(w, r, ai.ErrInvalid)
		return
	}
	data(w, r, 200, o.Response)
}
func (s *Server) complete(w http.ResponseWriter, r *http.Request, o operation, result, snapshot any, err error) {
	var raw, snap []byte
	state := "COMPLETED"
	code := ""
	status := 201
	retry := 0
	if err != nil {
		var a *ai.Error
		if !errors.As(err, &a) {
			a = ai.ErrInvalid
		}
		code, status, retry = a.Code, a.Status, a.RetryAfter
		state = "FAILED_UNCERTAIN"
	} else {
		raw, _ = json.Marshal(result)
		snap, _ = json.Marshal(snapshot)
		if v, ok := result.(map[string]any); ok && v["status"] != "READY" && v["recommendation_id"] != nil {
			status = 200
		}
	}
	// Persist the outcome even after the phone disconnects; a replay must not generate twice.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, e := s.DB.Exec(ctx, `UPDATE ai_operations o SET state=$1,response=$2,snapshot=$3,http_status=$4,error_code=$5,retry_after=$6 WHERE o.id=$7 AND o.user_id=$8 AND o.state='IN_PROGRESS' AND o.deleted_at IS NULL AND o.expires_at>now() AND (o.detection_id IS NULL OR EXISTS(SELECT 1 FROM ai_operations d WHERE d.id=o.detection_id AND d.user_id=o.user_id AND d.deleted_at IS NULL AND d.expires_at>now()))`, state, raw, snap, status, code, retry, o.ID, who(r).User.ID)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, r, 410, "AI_SESSION_EXPIRED", nil)
		return
	}
	if err != nil {
		aiFailure(w, r, err)
		return
	}
	data(w, r, status, result)
}
func (s *Server) aiDetection(w http.ResponseWriter, r *http.Request) {
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		fail(w, r, 503, "AI_BUSY", nil)
		return
	}
	if !s.aiEnabled(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImage+65536)
	defer r.Body.Close()
	reader, e := r.MultipartReader()
	if e != nil {
		fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", nil)
		return
	}
	var imageBytes []byte
	declaredMIME := ""
	clientID := ""
	seen := map[string]bool{}
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			fail(w, r, 413, "FILE_TOO_LARGE", nil)
			return
		}
		name := part.FormName()
		if seen[name] || !domain.Contains([]string{"image", "client_request_id"}, name) {
			part.Close()
			fail(w, r, 400, "INVALID_REQUEST", nil)
			return
		}
		seen[name] = true
		if name == "image" {
			contentType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil || (contentType != "image/jpeg" && contentType != "image/png") {
				part.Close()
				fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", nil)
				return
			}
			declaredMIME = contentType
			imageBytes, e = io.ReadAll(io.LimitReader(part, maxImage+1))
		} else {
			raw, err := io.ReadAll(io.LimitReader(part, 100))
			e = err
			clientID = string(raw)
		}
		part.Close()
		if e != nil || len(imageBytes) > maxImage {
			fail(w, r, 413, "FILE_TOO_LARGE", nil)
			return
		}
	}
	if !auth.UUID(clientID) || len(imageBytes) == 0 {
		fail(w, r, 422, "VALIDATION_ERROR", nil)
		return
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(imageBytes))
	if e != nil || (format != "jpeg" && format != "png") || declaredMIME != "image/"+format {
		fail(w, r, 422, "INVALID_IMAGE", nil)
		return
	}
	if cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 12000000 {
		fail(w, r, 422, "IMAGE_DIMENSIONS_EXCEEDED", nil)
		return
	}
	o, ok := s.claim(w, r, "DETECTION", clientID, auth.Digest(imageBytes), nil)
	if !ok {
		return
	}
	// Decoding and orientation happen only for the job owner; ingress stays in bounded memory.
	normalized, e := imaging.Decode(bytes.NewReader(imageBytes), imaging.AutoOrientation(true))
	if e != nil {
		s.complete(w, r, o, nil, nil, &ai.Error{Status: 422, Code: "INVALID_IMAGE"})
		return
	}
	imageBytes = nil
	var clean bytes.Buffer
	if e = jpeg.Encode(&clean, normalized, &jpeg.Options{Quality: 90}); e != nil || clean.Len() > maxImage {
		s.complete(w, r, o, nil, nil, &ai.Error{Status: 413, Code: "FILE_TOO_LARGE"})
		return
	}
	normalized = nil
	snapshot, e := s.AI.Detect(r.Context(), requestID(r), "image/jpeg", clean.Bytes())
	clean.Reset()
	if e != nil {
		s.complete(w, r, o, nil, nil, e)
		return
	}
	result := map[string]any{}
	for k, v := range snapshot {
		if k != "model" {
			result[k] = v
		}
	}
	result["detection_id"] = o.ID
	result["expires_at"] = o.Expires.Format(time.RFC3339Nano)
	if e = s.AI.Validate("DetectionSession", result); e != nil {
		s.complete(w, r, o, nil, nil, e)
		return
	}
	s.complete(w, r, o, result, snapshot, nil)
}
func (s *Server) aiRecommendation(w http.ResponseWriter, r *http.Request) {
	if !s.aiEnabled(w, r) {
		return
	}
	var in map[string]any
	if !decode(w, r, &in) {
		return
	}
	if s.AI.NormalizeInput(in) != nil {
		fail(w, r, 422, "VALIDATION_ERROR", nil)
		return
	}
	clientID, _ := in["client_request_id"].(string)
	if !auth.UUID(clientID) {
		fail(w, r, 422, "VALIDATION_ERROR", nil)
		return
	}
	var parent *string
	var selection any
	if in["input_source"] == "DETECTION" {
		id, _ := in["detection_id"].(string)
		if !auth.UUID(id) {
			fail(w, r, 422, "VALIDATION_ERROR", nil)
			return
		}
		parent = &id
		o, e := scanOperation(s.DB.QueryRow(r.Context(), `SELECT `+operationColumns+` FROM ai_operations WHERE id=$1 AND user_id=$2 AND operation='DETECTION'`, id, who(r).User.ID))
		if !s.checkOperation(w, r, o, e) {
			return
		}
		if o.State != "COMPLETED" {
			fail(w, r, 422, "INVALID_AI_REFERENCE", nil)
			return
		}
		var snapshot map[string]any
		if json.Unmarshal(o.Snapshot, &snapshot) != nil {
			aiFailure(w, r, ai.ErrInvalid)
			return
		}
		objectID := in["object_id"]
		var object map[string]any
		for _, entry := range snapshot["objects"].([]any) {
			candidate := entry.(map[string]any)
			if candidate["object_id"] == objectID {
				object = candidate
				break
			}
		}
		if object == nil {
			fail(w, r, 422, "INVALID_AI_REFERENCE", nil)
			return
		}
		if object["supported_for_review"] != true {
			fail(w, r, 422, "OBJECT_REQUIRES_MANUAL", nil)
			return
		}
		rawObject := map[string]any{}
		for _, key := range []string{"class_id", "raw_class", "confidence", "bbox"} {
			rawObject[key] = object[key]
		}
		model := snapshot["model"].(map[string]any)
		selection = map[string]any{"model_version": model["model_version"], "model_sha256": model["sha256"], "mapping_version": snapshot["mapping_version"], "class_names": model["class_names"], "object": rawObject}
	}
	raw, _ := json.Marshal(in)
	o, ok := s.claim(w, r, "RECOMMENDATION", clientID, auth.Digest(raw), parent)
	if !ok {
		return
	}
	contextData := map[string]any{"object_key": in["confirmed_object_key"], "category": in["confirmed_category"]}
	for _, key := range []string{"condition", "reuse_intention", "risk_flag", "risk_type", "recommendation_goal", "recycling_access", "allow_conditional"} {
		contextData[key] = in[key]
	}
	private := map[string]any{"context": contextData, "selection": selection}
	v, e := s.AI.Recommend(r.Context(), requestID(r), private)
	if e != nil {
		s.complete(w, r, o, nil, nil, e)
		return
	}
	v["recommendation_id"] = o.ID
	v["detection_id"] = in["detection_id"]
	v["object_id"] = in["object_id"]
	v["context_revision"] = in["context_revision"]
	v["confirmed_object_key"] = in["confirmed_object_key"]
	v["category"] = in["confirmed_category"]
	v["expires_at"] = o.Expires.Format(time.RFC3339Nano)
	if e = s.AI.Validate("RecommendationSession", v); e != nil {
		s.complete(w, r, o, nil, nil, e)
		return
	}
	s.complete(w, r, o, v, map[string]any{"request": in, "selection": selection, "result": v}, nil)
}
func (s *Server) aiDelete(w http.ResponseWriter, r *http.Request) {
	if !auth.UUID(r.PathValue("id")) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	ctx := r.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `SELECT id::text FROM ai_operations WHERE id=$1 AND user_id=$2 AND operation='DETECTION' FOR UPDATE`, r.PathValue("id"), who(r).User.ID).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", nil)
		return
	}
	if e == nil {
		_, e = tx.Exec(ctx, `UPDATE ai_operations SET deleted_at=COALESCE(deleted_at,now()),response=NULL,snapshot=NULL WHERE user_id=$1 AND (id=$2 OR detection_id=$2)`, who(r).User.ID, id)
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		s.internal(w, r, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) validateProvenance(w http.ResponseWriter, r *http.Request, tx pgx.Tx, in domain.LogInput) bool {
	p := in.AIProvenance
	if !auth.UUID(p.RecommendationID) || !p.UserConfirmedObject || !p.UserConfirmedMethod || in.Method == "REDUCE" {
		fail(w, r, 422, "AI_REFERENCE_INVALID", nil)
		return false
	}
	o, e := scanOperation(tx.QueryRow(r.Context(), `SELECT `+operationColumns+` FROM ai_operations WHERE id=$1 AND user_id=$2 AND operation='RECOMMENDATION' FOR UPDATE`, p.RecommendationID, who(r).User.ID))
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, r, 422, "AI_REFERENCE_INVALID", nil)
		return false
	}
	if !s.checkOperation(w, r, o, e) {
		return false
	}
	var v map[string]any
	if o.State != "COMPLETED" || json.Unmarshal(o.Response, &v) != nil || v["status"] != "READY" || v["category"] != in.Category || v["recommendation_scope"] != "HANDLING" {
		fail(w, r, 422, "AI_REFERENCE_INVALID", nil)
		return false
	}
	if v["prototype_only"] == true && !s.Config.AllowPrototype {
		fail(w, r, 422, "AI_REFERENCE_INVALID", nil)
		return false
	}
	var linked bool
	if e = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ai_log_metadata WHERE recommendation_id=$1)`, p.RecommendationID).Scan(&linked); e != nil {
		s.internal(w, r, e)
		return false
	}
	if linked {
		fail(w, r, 409, "AI_ALREADY_LINKED", nil)
		return false
	}
	return true
}
func (s *Server) linkProvenance(ctx context.Context, tx pgx.Tx, uid, id string, in domain.LogInput) error {
	_, e := tx.Exec(ctx, `INSERT INTO ai_log_metadata(waste_log_id,recommendation_id,user_id,snapshot,final_category,final_method,accepted) SELECT $1,id,$2,snapshot,$3,$4,response->>'recommended_method'=$4 FROM ai_operations WHERE id=$5 AND user_id=$2`, id, uid, in.Category, in.Method, in.AIProvenance.RecommendationID)
	return e
}
