ALTER TABLE client_billing_config DROP COLUMN IF EXISTS elba_bank_account_id;
ALTER TABLE client_billing_config DROP COLUMN IF EXISTS elba_contractor_id;
UPDATE client_billing_config SET markup = 3.0 WHERE markup IS NULL;
UPDATE client_billing_config SET min_price_rub = 500 WHERE min_price_rub IS NULL;
UPDATE client_billing_config SET rounding_rub = 50 WHERE rounding_rub IS NULL;
UPDATE client_billing_config SET fx_markup_percent = 5.0 WHERE fx_markup_percent IS NULL;
ALTER TABLE client_billing_config
    ALTER COLUMN markup SET NOT NULL,
    ALTER COLUMN min_price_rub SET NOT NULL,
    ALTER COLUMN rounding_rub SET NOT NULL,
    ALTER COLUMN fx_markup_percent SET NOT NULL;
DROP TABLE IF EXISTS client_billing_workspace_config;
