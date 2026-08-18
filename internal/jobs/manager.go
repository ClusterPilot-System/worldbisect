package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/id"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

type Handler func(context.Context, *model.Job) (string, error)

type Manager struct {
	store       *store.Store
	workers     int
	lease       time.Duration
	maxAttempts int
	handlers    map[string]Handler
	wake        chan struct{}
	wait        sync.WaitGroup
}

func New(dataStore *store.Store, workers int, lease time.Duration, maxAttempts int) *Manager {
	return &Manager{
		store:       dataStore,
		workers:     workers,
		lease:       lease,
		maxAttempts: maxAttempts,
		handlers:    make(map[string]Handler),
		wake:        make(chan struct{}, 1),
	}
}

func (manager *Manager) Register(kind string, handler Handler) {
	manager.handlers[kind] = handler
}

func (manager *Manager) Enqueue(kind string, payload []byte, principal, fingerprint, idempotencyKey, requestDigest string) (*model.Job, bool, error) {
	if idempotencyKey == "" {
		return nil, false, errors.New("idempotency key is required")
	}
	job, existing, err := manager.store.CreateJobIdempotent(store.CreateJobRequest{
		Job: model.Job{
			SchemaVersion: 3,
			ID:            id.New("job"),
			Type:          kind,
			State:         model.JobQueued,
			Payload:       append([]byte(nil), payload...),
			Principal:     principal,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		},
		PrincipalFingerprint: fingerprint,
		Route:                kind,
		IdempotencyKey:       idempotencyKey,
		RequestDigest:        requestDigest,
	})
	if err != nil {
		return nil, false, err
	}
	manager.Notify()
	return job, existing, nil
}

func (manager *Manager) Notify() {
	select {
	case manager.wake <- struct{}{}:
	default:
	}
}

func (manager *Manager) Run(ctx context.Context) {
	for index := 0; index < manager.workers; index++ {
		manager.wait.Add(1)
		go manager.worker(ctx, fmt.Sprintf("worker-%d", index+1))
	}
	manager.wait.Add(1)
	go manager.reaper(ctx)
}

func (manager *Manager) Wait() {
	manager.wait.Wait()
}

func (manager *Manager) worker(ctx context.Context, workerID string) {
	defer manager.wait.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-manager.wake:
		case <-ticker.C:
		}
		for {
			job, err := manager.store.ClaimNextJob(workerID, manager.lease, manager.maxAttempts)
			if err != nil {
				break
			}
			if job == nil {
				break
			}
			manager.execute(ctx, workerID, job)
		}
	}
}

func (manager *Manager) execute(ctx context.Context, workerID string, job *model.Job) {
	handler := manager.handlers[job.Type]
	if handler == nil {
		_ = manager.store.CompleteJob(job.ID, workerID, model.JobFailed, "", "no handler registered")
		return
	}
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(manager.lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if err := manager.store.HeartbeatJob(job.ID, workerID, manager.lease); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	result, err := handler(jobCtx, job)
	close(stopHeartbeat)
	if err != nil {
		_ = manager.store.CompleteJob(job.ID, workerID, model.JobFailed, "", err.Error())
		return
	}
	_ = manager.store.CompleteJob(job.ID, workerID, model.JobSucceeded, result, "")
}

func (manager *Manager) reaper(ctx context.Context) {
	defer manager.wait.Done()
	ticker := time.NewTicker(manager.lease / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, _ := manager.store.RequeueExpiredJobs(manager.maxAttempts)
			if count > 0 {
				manager.Notify()
			}
		}
	}
}
