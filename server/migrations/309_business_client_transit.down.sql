ALTER TABLE business_client
DROP CONSTRAINT IF EXISTS business_client_transit_tax_check;

ALTER TABLE business_client
DROP CONSTRAINT IF EXISTS business_client_transit_commission_check;

ALTER TABLE business_client
DROP COLUMN IF EXISTS transit_tax_percent,
DROP COLUMN IF EXISTS transit_commission_percent;
