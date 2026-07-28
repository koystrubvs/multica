-- Add `ready` status: period elapsed and prepared for human review before
-- close + Elba invoice. open -> ready -> closed -> invoiced -> paid.
ALTER TABLE client_billing_period
    DROP CONSTRAINT IF EXISTS client_billing_period_status_check;

ALTER TABLE client_billing_period
    ADD CONSTRAINT client_billing_period_status_check
    CHECK (status IN ('open', 'ready', 'closed', 'invoiced', 'paid'));
