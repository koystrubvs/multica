CREATE INDEX CONCURRENTLY IF NOT EXISTS business_transaction_match_transaction_idx ON business_transaction_match (business_id, transaction_id, status);
