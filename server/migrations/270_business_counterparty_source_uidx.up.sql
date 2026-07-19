CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_counterparty_source_uidx ON business_counterparty_classification (business_id, source, external_id);
