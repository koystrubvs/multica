CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_transaction_match_idempotency_uidx ON business_transaction_match (business_id, idempotency_key);
