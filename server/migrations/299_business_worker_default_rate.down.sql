ALTER TABLE business_worker DROP CONSTRAINT IF EXISTS business_worker_default_percent_check;
ALTER TABLE business_worker DROP CONSTRAINT IF EXISTS business_worker_default_role_check;
ALTER TABLE business_worker DROP COLUMN IF EXISTS default_percent;
ALTER TABLE business_worker DROP COLUMN IF EXISTS default_role;
