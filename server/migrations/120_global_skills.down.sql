DROP INDEX IF EXISTS idx_skill_global_owner;
DROP INDEX IF EXISTS skill_global_owner_name_key;
ALTER TABLE skill DROP CONSTRAINT IF EXISTS skill_global_requires_owner;
-- Fails if any global skills remain; delete them before rolling back.
ALTER TABLE skill ALTER COLUMN workspace_id SET NOT NULL;
