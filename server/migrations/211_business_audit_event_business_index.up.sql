CREATE INDEX CONCURRENTLY IF NOT EXISTS business_audit_event_business_created_idx ON business_audit_event (business_id, created_at DESC, id);
