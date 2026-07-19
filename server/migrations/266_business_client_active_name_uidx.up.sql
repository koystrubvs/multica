CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_client_active_name_uidx ON business_client (business_id, lower(canonical_name)) WHERE archived_at IS NULL;
