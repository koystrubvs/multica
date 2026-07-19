CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_client_request_idempotency_uidx ON business_client_request (business_id, idempotency_key);
