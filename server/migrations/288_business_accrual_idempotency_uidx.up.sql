CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_accrual_idempotency_uidx ON business_accrual (business_id, idempotency_key);
