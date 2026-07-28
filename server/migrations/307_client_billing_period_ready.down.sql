-- Revert ready periods to open before tightening the check.
UPDATE client_billing_period SET status = 'open', updated_at = now()
WHERE status = 'ready';

ALTER TABLE client_billing_period
    DROP CONSTRAINT IF EXISTS client_billing_period_status_check;

ALTER TABLE client_billing_period
    ADD CONSTRAINT client_billing_period_status_check
    CHECK (status IN ('open', 'closed', 'invoiced', 'paid'));
