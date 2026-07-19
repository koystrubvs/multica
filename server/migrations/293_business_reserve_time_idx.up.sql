CREATE INDEX CONCURRENTLY IF NOT EXISTS business_reserve_time_idx ON business_reserve_ledger (business_id, occurred_at DESC);
