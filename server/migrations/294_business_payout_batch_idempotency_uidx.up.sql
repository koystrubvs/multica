CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_payout_batch_idempotency_uidx ON business_payout_batch (business_id, idempotency_key);
