CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_bank_outbox_idempotency_uidx ON business_bank_outbox (business_id, idempotency_key);
