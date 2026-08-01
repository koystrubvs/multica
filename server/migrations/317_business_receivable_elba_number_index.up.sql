-- The lookup the bank matcher runs for every inbound payment that names an
-- invoice: business plus number, narrowed by date in the query. Partial, because
-- only lines that have been invoiced carry a number.
CREATE INDEX CONCURRENTLY IF NOT EXISTS business_receivable_elba_number_idx
  ON business_receivable (business_id, elba_invoice_number)
  WHERE elba_invoice_number IS NOT NULL;
