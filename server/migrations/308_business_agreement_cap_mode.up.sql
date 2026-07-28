ALTER TABLE business_agreement
ADD COLUMN cap_mode TEXT NOT NULL DEFAULT 'strict';

ALTER TABLE business_agreement
ADD CONSTRAINT business_agreement_cap_mode_check
CHECK (cap_mode IN ('strict', 'advisory'));
