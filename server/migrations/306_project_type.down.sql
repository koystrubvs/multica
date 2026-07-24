ALTER TABLE project
    DROP CONSTRAINT IF EXISTS project_project_type_check,
    DROP COLUMN IF EXISTS project_type;
