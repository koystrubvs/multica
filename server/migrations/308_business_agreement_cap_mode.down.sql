ALTER TABLE business_agreement
DROP CONSTRAINT IF EXISTS business_agreement_cap_mode_check;

ALTER TABLE business_agreement
DROP COLUMN IF EXISTS cap_mode;
