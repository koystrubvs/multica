CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_accrual_adjustment_idempotency_uidx ON business_accrual_adjustment (business_id, idempotency_key);
