ALTER TABLE client_billing_charge DROP COLUMN IF EXISTS period_id;
DROP TABLE IF EXISTS client_billing_period;
