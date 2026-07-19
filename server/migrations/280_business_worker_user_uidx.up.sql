CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_worker_user_uidx ON business_worker (business_id, user_id) WHERE user_id IS NOT NULL;
