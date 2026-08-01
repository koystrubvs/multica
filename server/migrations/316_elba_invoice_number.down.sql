ALTER TABLE business_receivable
  DROP COLUMN IF EXISTS elba_invoice_number,
  DROP COLUMN IF EXISTS elba_invoice_date;

ALTER TABLE client_billing_period
  DROP COLUMN IF EXISTS elba_invoice_number,
  DROP COLUMN IF EXISTS elba_invoice_date;
