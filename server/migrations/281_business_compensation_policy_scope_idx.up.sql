CREATE INDEX CONCURRENTLY IF NOT EXISTS business_compensation_policy_scope_idx ON business_compensation_policy (business_id, service_type, pool, effective_from DESC) WHERE status = 'active';
