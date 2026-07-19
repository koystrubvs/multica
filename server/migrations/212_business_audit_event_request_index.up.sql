CREATE INDEX CONCURRENTLY IF NOT EXISTS business_audit_event_request_idx ON business_audit_event (request_id) WHERE request_id IS NOT NULL;
