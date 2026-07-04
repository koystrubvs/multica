DROP TABLE IF EXISTS client_billing_dispute;
ALTER TABLE client_billing_charge DROP COLUMN IF EXISTS source;
DROP INDEX IF EXISTS idx_client_billing_charge_issue;
-- Best-effort: fails when metering has already created several charges for
-- one issue — v2 data cannot be squeezed back into the v1 shape.
ALTER TABLE client_billing_charge ADD CONSTRAINT client_billing_charge_issue_id_key UNIQUE (issue_id);
