CREATE INDEX CONCURRENTLY IF NOT EXISTS business_recurring_cost_active_idx ON business_recurring_cost (business_id, starts_on, ends_on, charge_day) WHERE status = 'active';
