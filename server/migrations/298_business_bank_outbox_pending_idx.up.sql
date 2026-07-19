CREATE INDEX CONCURRENTLY IF NOT EXISTS business_bank_outbox_pending_idx ON business_bank_outbox (status, next_attempt_at) WHERE status IN ('pending', 'failed');
