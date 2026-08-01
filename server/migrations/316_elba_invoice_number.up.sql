-- 316: the number Elba puts on the invoice, kept next to the document id.
--
-- Payments name the invoice in plain text — "Оплата по счету № 93 от 30 июня
-- 2026" — so the number is the only signal that ties money to one plan line
-- exactly. The document id we already store is a UUID and never appears in a
-- bank statement, which is why matching has been guessing from dates and
-- amounts and has settled the wrong line twice on real money.
--
-- The issue date comes along because numbering restarts every year: № 16 exists
-- in 2023, 2025 and 2026, for three different clients, and № 484 in both 2023
-- and 2024. The number alone is not an identifier.
--
-- business_receivable.invoice_on already exists but means something else — the
-- planned invoice day derived from the agreement's invoice_day, filled when the
-- calendar is generated, long before any document exists. The day Elba actually
-- issued the invoice needs its own column.
ALTER TABLE client_billing_period
  ADD COLUMN IF NOT EXISTS elba_invoice_number TEXT,
  ADD COLUMN IF NOT EXISTS elba_invoice_date   DATE;

ALTER TABLE business_receivable
  ADD COLUMN IF NOT EXISTS elba_invoice_number TEXT,
  ADD COLUMN IF NOT EXISTS elba_invoice_date   DATE;
