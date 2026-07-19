CREATE INDEX CONCURRENTLY IF NOT EXISTS business_company_cost_date_idx ON business_company_cost (business_id, incurred_on DESC, category) WHERE voided_at IS NULL;
