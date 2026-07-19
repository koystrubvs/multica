CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_client_payer_elba_uidx ON business_client_payer (business_id, workspace_id, elba_contractor_id) WHERE elba_contractor_id IS NOT NULL;
