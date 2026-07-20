package scheduler

import (
	"context"
	"testing"
	"time"
)

type stubModulbankSyncRunner struct{}

func (stubModulbankSyncRunner) Run(context.Context) (int64, map[string]any, error) {
	return 3, map[string]any{"mode": "incremental"}, nil
}

func TestModulbankOperationsSyncJob(t *testing.T) {
	job := ModulbankOperationsSyncJob(stubModulbankSyncRunner{}, 15*time.Minute)
	if err := job.validate(); err != nil {
		t.Fatal(err)
	}
	if job.Name != JobNameModulbankOperationsSync || job.Cadence != 15*time.Minute {
		t.Fatalf("unexpected job: %#v", job)
	}
	result, err := job.Handler(context.Background(), HandlerInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected != 3 || result.Result["mode"] != "incremental" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
