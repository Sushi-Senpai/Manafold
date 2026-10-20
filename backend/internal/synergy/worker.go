package synergy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "manafold-backend/internal/db/generated"
)

// workerLockKey is the Postgres advisory lock the one worker holds, so the
// 6 s Archidekt pace holds across API instances and the CLI (SYN-033).
const workerLockKey int64 = 0x4d46535957

// pollInterval is how long an idle worker waits before checking the queue
// again when nothing wakes it; lockRetry is how long a worker that lost the
// lock waits before trying again. Vars so tests can shorten them.
var (
	pollInterval = 30 * time.Second
	lockRetry    = time.Minute
)

// ErrWorkerBusy means another process holds the worker lock (SYN-033, SYN-037).
var ErrWorkerBusy = errors.New("another synergy worker holds the lock")

// DrainResult reports what Drain did.
type DrainResult struct {
	Jobs     int // jobs run to completion
	Requests int // Archidekt requests those jobs made
}

// RunWorker drains the job queue until ctx is done. It does nothing unless the
// service is enabled (SYN-001). It runs only while it holds the worker lock; a
// process that cannot take the lock retries later (SYN-033).
//
// @spec SYN-001, SYN-033
func (s *Service) RunWorker(ctx context.Context) {
	if !s.opts.Enabled {
		return
	}
	for ctx.Err() == nil {
		err := s.withWorkerLock(ctx, func(conn *pgxpool.Conn) error {
			if err := s.q.RequeueRunningSynergyJobs(ctx); err != nil {
				return err
			}
			return s.processQueue(ctx, false, conn, &DrainResult{})
		})
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrWorkerBusy) {
			s.opts.Logf("synergy: another worker holds the lock; retrying in %v", lockRetry)
		} else if err != nil {
			s.opts.Logf("synergy: worker stopped: %v", err)
		}
		_ = sleepCtx(ctx, lockRetry)
	}
}

// Drain runs every queued job through the worker code and returns, for the
// CLI. It stops at the first failed job and returns its error, and returns
// ErrWorkerBusy if another process holds the worker lock (SYN-037).
//
// @spec SYN-033, SYN-037
func (s *Service) Drain(ctx context.Context) (DrainResult, error) {
	var res DrainResult
	err := s.withWorkerLock(ctx, func(conn *pgxpool.Conn) error {
		if err := s.q.RequeueRunningSynergyJobs(ctx); err != nil {
			return err
		}
		return s.processQueue(ctx, true, conn, &res)
	})
	return res, err
}

// withWorkerLock takes the session advisory lock on a dedicated connection,
// runs fn, and releases the lock.
func (s *Service) withWorkerLock(ctx context.Context, fn func(conn *pgxpool.Conn) error) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, workerLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return ErrWorkerBusy
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, workerLockKey)
	}()
	return fn(conn)
}

// processQueue claims and runs jobs oldest first. In once mode it returns
// when the queue is empty or a job fails; otherwise it waits for new jobs and
// pauses for the failure cooldown after a failed job (SYN-033, SYN-036).
func (s *Service) processQueue(ctx context.Context, once bool, lock *pgxpool.Conn, res *DrainResult) error {
	for ctx.Err() == nil {
		// A dropped lock connection means the lock is gone; stop rather than
		// run unguarded.
		if err := lock.Ping(ctx); err != nil {
			return fmt.Errorf("worker lock connection: %w", err)
		}
		job, err := s.q.ClaimNextSynergyJob(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			if once {
				return nil
			}
			select {
			case <-ctx.Done():
			case <-s.wake:
			case <-time.After(pollInterval):
			}
			continue
		}
		if err != nil {
			return err
		}
		requests, err := s.runJob(ctx, job)
		if ctx.Err() != nil {
			return nil // shutdown: the job stays running and is requeued on the next start
		}
		res.Requests += requests
		if err != nil {
			if once {
				return err
			}
			s.opts.Logf("synergy: job failed; pausing %v: %v", s.opts.FailureCooldown, err)
			_ = sleepCtx(ctx, s.opts.FailureCooldown)
			continue
		}
		res.Jobs++
	}
	return nil
}

// runJob gathers one commander: its colour-identity baseline first when that
// is missing or past the TTL, then the commander, each written when its sample
// completes. It records the job's outcome and returns the requests made
// (SYN-034, SYN-035). A cancelled context leaves the job unrecorded.
//
// @spec SYN-034, SYN-035
func (s *Service) runJob(ctx context.Context, job db.ClaimNextSynergyJobRow) (requests int, err error) {
	defer func() {
		if ctx.Err() != nil {
			return
		}
		params := db.FinishSynergyJobParams{ID: job.ID, Status: "succeeded", RequestsMade: int32(requests)}
		if err != nil {
			params.Status = "failed"
			params.Error = pgtype.Text{String: err.Error(), Valid: true}
		}
		if ferr := s.q.FinishSynergyJob(context.WithoutCancel(ctx), params); ferr != nil {
			s.opts.Logf("synergy: record job outcome: %v", ferr)
		}
	}()

	card, err := s.q.GetCardByScryfallOracleID(ctx, job.CommanderOracleID)
	if err != nil {
		return 0, fmt.Errorf("resolve commander: %w", err)
	}
	identity := card.CommanderColorIdentity
	if identity == nil {
		identity = card.ColorIdentity
	}
	cmd := commanderRef{
		oracleID: job.CommanderOracleID,
		oracle:   uuidString(job.CommanderOracleID),
		name:     card.Name,
		identity: CanonicalIdentity(identity),
	}

	targets, err := s.planJob(ctx, cmd)
	if err != nil {
		return 0, err
	}
	budget := retryAllowance
	for _, t := range targets {
		budget += t.worstCase(s.opts)
	}
	s.client.beginJob(budget)

	for _, t := range targets {
		smp, err := sampleTarget(ctx, s.client, t, s.opts)
		if err != nil {
			return s.client.requests, fmt.Errorf("%s: %w", t.label(), err)
		}
		if err := writeTarget(ctx, s.pool, t, smp, time.Now()); err != nil {
			return s.client.requests, fmt.Errorf("%s: write: %w", t.label(), err)
		}
		s.opts.Logf("synergy: gathered %s: %d decks (%d duplicates skipped), %d cards",
			t.label(), smp.decks, smp.duplicates, len(smp.cardDecks))
	}
	return s.client.requests, nil
}

// planJob lists the targets a job gathers: the identity's baseline when it is
// missing or not fresher than the TTL, then the commander (SYN-011, SYN-034).
func (s *Service) planJob(ctx context.Context, cmd commanderRef) ([]target, error) {
	var targets []target
	fetched, err := s.q.GetSynergyBaselineFreshness(ctx, cmd.identity)
	stale := errors.Is(err, pgx.ErrNoRows) || (err == nil && time.Since(fetched.Time) >= s.opts.TTL)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if stale {
		rows, err := s.q.ListBaselineCommanders(ctx, db.ListBaselineCommandersParams{
			Identity: identityArray(cmd.identity),
			Exclude:  cmd.oracleID,
			Lim:      int32(s.opts.BaselineCommanders),
		})
		if err != nil {
			return nil, err
		}
		t := target{baseline: true, identity: cmd.identity}
		for _, r := range rows {
			t.sources = append(t.sources, commanderRef{
				oracleID: r.ScryfallOracleID,
				oracle:   uuidString(r.ScryfallOracleID),
				name:     r.Name,
				identity: cmd.identity,
			})
		}
		targets = append(targets, t)
	}
	return append(targets, target{identity: cmd.identity, commander: cmd}), nil
}
