CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_bank_import_idempotency_uidx ON business_bank_import_batch (business_id, idempotency_key);
