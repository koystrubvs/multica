ALTER TABLE member DROP CONSTRAINT IF EXISTS member_access_scope_check;
ALTER TABLE member DROP COLUMN IF EXISTS access_scope;
