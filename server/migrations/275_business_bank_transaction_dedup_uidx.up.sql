CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_bank_transaction_dedup_uidx ON business_bank_transaction (business_id, dedup_key);
