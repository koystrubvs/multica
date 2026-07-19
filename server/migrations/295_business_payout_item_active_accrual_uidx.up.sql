CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_payout_item_active_accrual_uidx ON business_payout_item (accrual_id) WHERE status IN ('pending', 'submitted');
