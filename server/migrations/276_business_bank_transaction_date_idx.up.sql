CREATE INDEX CONCURRENTLY IF NOT EXISTS business_bank_transaction_date_idx ON business_bank_transaction (business_id, booked_on DESC, classification, direction) WHERE voided_at IS NULL;
