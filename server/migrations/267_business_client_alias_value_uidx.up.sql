CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_client_alias_value_uidx ON business_client_alias (business_id, source, alias_type, normalized_value);
