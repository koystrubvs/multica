CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_reserve_idempotency_uidx ON business_reserve_ledger (business_id, idempotency_key);
