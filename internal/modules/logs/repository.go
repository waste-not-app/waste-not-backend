package logs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	CreateIdempotent(ctx context.Context, userID, timezone, today, hash string, in domain.LogInput, now time.Time, allowPrototype bool) (LogWithProgressRes, bool, error)
	GetByID(ctx context.Context, userID, id string) (domain.Log, error)
	List(ctx context.Context, userID string, q ListLogsQuery) ([]domain.Log, error)
	Update(ctx context.Context, userID, timezone, today, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error)
	Delete(ctx context.Context, userID, id string, version int) error
}

type repository struct {
	dbPool *pgxpool.Pool
	q      *db.Queries
}

func NewRepository(dbPool *pgxpool.Pool) Repository {
	return &repository{
		dbPool: dbPool,
		q:      db.New(dbPool),
	}
}

func toDomainLog(id, category, method, qtyKind string, weightG, itemCount *int, actDate, actTz, notes string, version int, createdAt, updatedAt time.Time) domain.Log {
	return domain.Log{
		ID:               id,
		Category:         category,
		Method:           method,
		QuantityKind:     qtyKind,
		WeightG:          weightG,
		ItemCount:        itemCount,
		ActivityDate:     actDate,
		ActivityTimezone: actTz,
		Notes:            notes,
		Version:          version,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}
}

func isDuplicate(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p) && p.Code == "23505"
}

func (r *repository) validateProvenance(ctx context.Context, qtx *db.Queries, userID string, in domain.LogInput, now time.Time, allowPrototype bool) error {
	p := in.AIProvenance
	if !auth.UUID(p.RecommendationID) || !p.UserConfirmedObject || !p.UserConfirmedMethod || in.Method == "REDUCE" {
		return ErrAIReferenceInvalid
	}

	opRow, err := qtx.LockRecommendationForProvenance(ctx, db.LockRecommendationForProvenanceParams{
		ID:     p.RecommendationID,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAIReferenceInvalid
	}
	if err != nil {
		return err
	}
	if opRow.Deleted || !opRow.ExpiresAt.After(now) {
		return ErrAISessionExpired
	}

	var v map[string]any
	if opRow.State != "COMPLETED" || json.Unmarshal(opRow.Response, &v) != nil || v["status"] != "READY" || v["category"] != in.Category || v["recommendation_scope"] != "HANDLING" {
		return ErrAIReferenceInvalid
	}
	if v["prototype_only"] == true && !allowPrototype {
		return ErrAIReferenceInvalid
	}

	linked, err := qtx.IsRecommendationLinked(ctx, p.RecommendationID)
	if err != nil {
		return err
	}
	if linked {
		return ErrAIAlreadyLinked
	}
	return nil
}

func (r *repository) CreateIdempotent(ctx context.Context, userID, timezone, today, hash string, in domain.LogInput, now time.Time, allowPrototype bool) (LogWithProgressRes, bool, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return LogWithProgressRes{}, false, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	if err = qtx.AcquireLogIdempotencyLock(ctx, userID+":"+in.ClientRequestID); err != nil {
		return LogWithProgressRes{}, false, err
	}

	existing, err := qtx.GetLogByClientRequestID(ctx, db.GetLogByClientRequestIDParams{
		UserID:          userID,
		ClientRequestID: in.ClientRequestID,
	})
	if err == nil {
		if existing.RequestHash != hash {
			return LogWithProgressRes{}, false, ErrIdempotencyConflict
		}
		if existing.Deleted {
			return LogWithProgressRes{}, false, ErrLogDeleted
		}
		lRow, e := qtx.GetLogByIDAnyState(ctx, db.GetLogByIDAnyStateParams{
			ID:     existing.ID,
			UserID: userID,
		})
		if e != nil {
			return LogWithProgressRes{}, false, e
		}
		stats, e := qtx.GetUserProgressStats(ctx, userID)
		if e == nil {
			e = tx.Commit(ctx)
		}
		return LogWithProgressRes{
			Log:      toDomainLog(lRow.ID, lRow.Category, lRow.Method, lRow.QuantityKind, lRow.WeightG, lRow.ItemCount, lRow.ActivityDate, lRow.ActivityTimezone, lRow.Notes, lRow.Version, lRow.CreatedAt, lRow.UpdatedAt),
			Progress: domain.MakeProgress(stats.Count, stats.ActivityDates, today),
		}, false, e
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LogWithProgressRes{}, false, err
	}

	if fields := in.Validate(today, ""); len(fields) > 0 {
		return LogWithProgressRes{}, false, &ValidationError{Fields: fields}
	}

	if in.AIProvenance != nil {
		if err = r.validateProvenance(ctx, qtx, userID, in, now, allowPrototype); err != nil {
			return LogWithProgressRes{}, false, err
		}
	}

	id := uuid.NewString()
	lRow, err := qtx.CreateWasteLog(ctx, db.CreateWasteLogParams{
		ID:               id,
		UserID:           userID,
		Category:         in.Category,
		Method:           in.Method,
		QuantityKind:     in.QuantityKind,
		WeightG:          in.WeightG,
		ItemCount:        in.ItemCount,
		ActivityDate:     in.ActivityDate,
		ActivityTimezone: timezone,
		Notes:            in.Notes,
		ClientRequestID:  in.ClientRequestID,
		RequestHash:      hash,
	})
	if err != nil {
		return LogWithProgressRes{}, false, err
	}

	if in.AIProvenance != nil {
		err = qtx.LinkAIProvenance(ctx, db.LinkAIProvenanceParams{
			WasteLogID:       id,
			UserID:           userID,
			FinalCategory:    in.Category,
			FinalMethod:      in.Method,
			RecommendationID: in.AIProvenance.RecommendationID,
		})
		if isDuplicate(err) {
			return LogWithProgressRes{}, false, ErrAIAlreadyLinked
		}
		if err != nil {
			return LogWithProgressRes{}, false, err
		}
	}

	stats, err := qtx.GetUserProgressStats(ctx, userID)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return LogWithProgressRes{
		Log:      toDomainLog(lRow.ID, lRow.Category, lRow.Method, lRow.QuantityKind, lRow.WeightG, lRow.ItemCount, lRow.ActivityDate, lRow.ActivityTimezone, lRow.Notes, lRow.Version, lRow.CreatedAt, lRow.UpdatedAt),
		Progress: domain.MakeProgress(stats.Count, stats.ActivityDates, today),
	}, true, err
}

func (r *repository) GetByID(ctx context.Context, userID, id string) (domain.Log, error) {
	lRow, err := r.q.GetActiveLogByID(ctx, db.GetActiveLogByIDParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Log{}, ErrLogNotFound
	}
	return toDomainLog(lRow.ID, lRow.Category, lRow.Method, lRow.QuantityKind, lRow.WeightG, lRow.ItemCount, lRow.ActivityDate, lRow.ActivityTimezone, lRow.Notes, lRow.Version, lRow.CreatedAt, lRow.UpdatedAt), err
}

func (r *repository) List(ctx context.Context, userID string, q ListLogsQuery) ([]domain.Log, error) {
	rows, err := r.q.ListActiveLogs(ctx, db.ListActiveLogsParams{
		UserID:    userID,
		Category:  q.Category,
		Method:    q.Method,
		FromDate:  q.From,
		ToDate:    q.To,
		DatePos:   q.DatePos,
		IDPos:     q.IDPos,
		PageLimit: q.Limit + 1,
	})
	if err != nil {
		return nil, err
	}
	logs := make([]domain.Log, 0, len(rows))
	for _, r := range rows {
		logs = append(logs, toDomainLog(r.ID, r.Category, r.Method, r.QuantityKind, r.WeightG, r.ItemCount, r.ActivityDate, r.ActivityTimezone, r.Notes, r.Version, r.CreatedAt, r.UpdatedAt))
	}
	return logs, nil
}

func (r *repository) Update(ctx context.Context, userID, timezone, today, id string, version int, patch map[string]json.RawMessage) (LogWithProgressRes, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return LogWithProgressRes{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	oldRow, err := qtx.LockActiveLogByID(ctx, db.LockActiveLogByIDParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return LogWithProgressRes{}, ErrLogNotFound
	}
	if err != nil {
		return LogWithProgressRes{}, err
	}
	if oldRow.Version != version {
		return LogWithProgressRes{}, ErrVersionConflict
	}

	in := domain.LogInput{
		Category:     oldRow.Category,
		Method:       oldRow.Method,
		QuantityKind: oldRow.QuantityKind,
		WeightG:      oldRow.WeightG,
		ItemCount:    oldRow.ItemCount,
		ActivityDate: oldRow.ActivityDate,
		Notes:        oldRow.Notes,
	}
	targets := map[string]any{
		"category":      &in.Category,
		"method":        &in.Method,
		"quantity_kind": &in.QuantityKind,
		"weight_g":      &in.WeightG,
		"item_count":    &in.ItemCount,
		"activity_date": &in.ActivityDate,
		"notes":         &in.Notes,
	}
	for key, target := range targets {
		if raw, exists := patch[key]; exists {
			if string(raw) == "null" {
				switch key {
				case "notes":
					in.Notes = ""
					continue
				case "weight_g", "item_count":
				default:
					return LogWithProgressRes{}, &ValidationError{Fields: map[string]string{key: "Isian wajib diisi."}}
				}
			}
			if err = json.Unmarshal(raw, target); err != nil {
				return LogWithProgressRes{}, ErrInvalidPatchRequest
			}
		}
	}

	if fields := in.Validate(today, oldRow.ActivityDate); len(fields) > 0 {
		return LogWithProgressRes{}, &ValidationError{Fields: fields}
	}

	updatedRow, err := qtx.UpdateWasteLog(ctx, db.UpdateWasteLogParams{
		Category:     in.Category,
		Method:       in.Method,
		QuantityKind: in.QuantityKind,
		WeightG:      in.WeightG,
		ItemCount:    in.ItemCount,
		ActivityDate: in.ActivityDate,
		Notes:        in.Notes,
		ID:           oldRow.ID,
		UserID:       userID,
		Version:      version,
	})
	if err != nil {
		return LogWithProgressRes{}, err
	}

	stats, err := qtx.GetUserProgressStats(ctx, userID)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return LogWithProgressRes{
		Log:      toDomainLog(updatedRow.ID, updatedRow.Category, updatedRow.Method, updatedRow.QuantityKind, updatedRow.WeightG, updatedRow.ItemCount, updatedRow.ActivityDate, updatedRow.ActivityTimezone, updatedRow.Notes, updatedRow.Version, updatedRow.CreatedAt, updatedRow.UpdatedAt),
		Progress: domain.MakeProgress(stats.Count, stats.ActivityDates, today),
	}, err
}

func (r *repository) Delete(ctx context.Context, userID, id string, version int) error {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	row, err := qtx.LockLogVersionForDelete(ctx, db.LockLogVersionForDeleteParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLogNotFound
	}
	if err != nil {
		return err
	}
	if !row.Deleted && row.Version != version {
		return ErrVersionConflict
	}
	if !row.Deleted {
		err = qtx.SoftDeleteWasteLog(ctx, db.SoftDeleteWasteLogParams{
			ID:      id,
			UserID:  userID,
			Version: version,
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}
