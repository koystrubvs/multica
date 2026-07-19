CREATE INDEX CONCURRENTLY IF NOT EXISTS business_receivable_due_idx ON business_receivable (business_id, status, due_on, invoice_on);
