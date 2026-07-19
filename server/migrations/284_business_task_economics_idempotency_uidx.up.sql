CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_task_economics_idempotency_uidx ON business_task_economics (business_id, idempotency_key);
