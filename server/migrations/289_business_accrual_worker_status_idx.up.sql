CREATE INDEX CONCURRENTLY IF NOT EXISTS business_accrual_worker_status_idx ON business_accrual (business_id, worker_id, status, reserve_due_on);
