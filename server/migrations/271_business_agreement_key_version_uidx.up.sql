CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_agreement_key_version_uidx ON business_agreement (business_id, agreement_key, version);
