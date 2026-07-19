CREATE INDEX CONCURRENTLY IF NOT EXISTS business_payout_item_batch_idx ON business_payout_item (business_id, payout_batch_id, status);
