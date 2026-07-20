package scheduler

import (
	"context"
	"time"
)

const JobNameModulbankOperationsSync = "sync_modulbank_operations"

type ModulbankSyncRunner interface {
	Run(ctx context.Context) (rowsAffected int64, result map[string]any, err error)
}

func ModulbankOperationsSyncJob(runner ModulbankSyncRunner, cadence time.Duration) JobSpec {
	if cadence <= 0 {
		cadence = 15 * time.Minute
	}
	return JobSpec{
		Name:              JobNameModulbankOperationsSync,
		Cadence:           cadence,
		ScheduleDelay:     0,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
		},
		Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			rows, result, err := runner.Run(ctx)
			if err != nil {
				return HandlerResult{}, err
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: rows, Result: result}, nil
		},
	}
}
