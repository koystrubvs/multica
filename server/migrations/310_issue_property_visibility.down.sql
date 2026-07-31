ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_visibility_check;
ALTER TABLE issue_property DROP COLUMN IF EXISTS visibility;
