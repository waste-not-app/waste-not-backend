package ai

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	Claim(ctx context.Context, userID, kind, clientID, hash string, parent *string, now time.Time) (Operation, bool, error)
	GetOperation(ctx context.Context, userID, id string, kindFilter string, now time.Time) (Operation, error)
	Complete(ctx context.Context, userID, opID, state string, rawResp, rawSnap []byte, status int, code string, retry int) (bool, error)
	DeleteDetection(ctx context.Context, userID, id string) error
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

func toOperation(id, state, hash string, resp, snap []byte, status int, code string, retry int, expires time.Time, deleted bool) Operation {
	return Operation{
		ID:         id,
		State:      state,
		Hash:       hash,
		Response:   resp,
		Snapshot:   snap,
		Status:     status,
		Code:       code,
		RetryAfter: retry,
		Expires:    expires,
		Deleted:    deleted,
	}
}

func checkOpState(o Operation, err error, now time.Time) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}
	if o.Deleted || !o.Expires.After(now) {
		return ErrAISessionExpired
	}
	return nil
}

func (r *repository) Claim(ctx context.Context, userID, kind, clientID, hash string, parent *string, now time.Time) (Operation, bool, error) {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return Operation{}, false, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	expires := now.UTC().Add(24 * time.Hour)
	if parent != nil {
		pRow, e := qtx.LockDetectionOperation(ctx, db.LockDetectionOperationParams{
			ID:     *parent,
			UserID: userID,
		})
		pOp := toOperation(pRow.ID, pRow.State, pRow.RequestHash, pRow.Response, pRow.Snapshot, pRow.HttpStatus, pRow.ErrorCode, pRow.RetryAfter, pRow.ExpiresAt, pRow.Deleted)
		if chkErr := checkOpState(pOp, e, now); chkErr != nil {
			return Operation{}, false, chkErr
		}
		expires = pOp.Expires
	}

	id := uuid.NewString()
	affected, err := qtx.InsertAIOperationIdempotent(ctx, db.InsertAIOperationIdempotentParams{
		ID:              id,
		UserID:          userID,
		Operation:       kind,
		ClientRequestID: clientID,
		RequestHash:     hash,
		DetectionID:     parent,
		ExpiresAt:       expires,
	})
	if err != nil {
		return Operation{}, false, err
	}

	oRow, err := qtx.GetAIOperationByClientRequestID(ctx, db.GetAIOperationByClientRequestIDParams{
		UserID:          userID,
		Operation:       kind,
		ClientRequestID: clientID,
	})
	o := toOperation(oRow.ID, oRow.State, oRow.RequestHash, oRow.Response, oRow.Snapshot, oRow.HttpStatus, oRow.ErrorCode, oRow.RetryAfter, oRow.ExpiresAt, oRow.Deleted)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return Operation{}, false, err
	}
	if chkErr := checkOpState(o, nil, now); chkErr != nil {
		return Operation{}, false, chkErr
	}
	if o.Hash != hash {
		return Operation{}, false, ErrIdempotencyConflict
	}
	return o, affected > 0, nil
}

func (r *repository) GetOperation(ctx context.Context, userID, id string, kindFilter string, now time.Time) (Operation, error) {
	var o Operation
	var err error
	if kindFilter != "" {
		row, e := r.q.GetAIOperationByIDAndKind(ctx, db.GetAIOperationByIDAndKindParams{
			ID:        id,
			UserID:    userID,
			Operation: kindFilter,
		})
		err = e
		o = toOperation(row.ID, row.State, row.RequestHash, row.Response, row.Snapshot, row.HttpStatus, row.ErrorCode, row.RetryAfter, row.ExpiresAt, row.Deleted)
	} else {
		row, e := r.q.GetAIOperationByID(ctx, db.GetAIOperationByIDParams{
			ID:     id,
			UserID: userID,
		})
		err = e
		o = toOperation(row.ID, row.State, row.RequestHash, row.Response, row.Snapshot, row.HttpStatus, row.ErrorCode, row.RetryAfter, row.ExpiresAt, row.Deleted)
	}
	if chkErr := checkOpState(o, err, now); chkErr != nil {
		return Operation{}, chkErr
	}
	return o, nil
}

func (r *repository) Complete(ctx context.Context, userID, opID, state string, rawResp, rawSnap []byte, status int, code string, retry int) (bool, error) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var httpStatus *int
	if status > 0 {
		httpStatus = &status
	}
	var retryAfter *int
	if retry > 0 {
		retryAfter = &retry
	}

	affected, err := r.q.CompleteAIOperation(bgCtx, db.CompleteAIOperationParams{
		State:      state,
		Response:   rawResp,
		Snapshot:   rawSnap,
		HttpStatus: httpStatus,
		ErrorCode:  code,
		RetryAfter: retryAfter,
		ID:         opID,
		UserID:     userID,
	})
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *repository) DeleteDetection(ctx context.Context, userID, id string) error {
	tx, err := r.dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	foundID, err := qtx.LockDetectionIDForDelete(ctx, db.LockDetectionIDForDeleteParams{
		ID:     id,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOperationNotFound
	}
	if err == nil {
		err = qtx.SoftDeleteDetectionCascade(ctx, db.SoftDeleteDetectionCascadeParams{
			UserID: userID,
			ID:     foundID,
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}
