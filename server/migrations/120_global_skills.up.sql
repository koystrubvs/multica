-- Global skills: a skill owned by a user and available across every workspace
-- that user belongs to, instead of being scoped to a single workspace. A skill
-- is "global" when workspace_id IS NULL; created_by is its owner. Workspace
-- skills (workspace_id NOT NULL) keep their existing semantics and remain
-- guarded by the UNIQUE(workspace_id, name) constraint from migration 008.

ALTER TABLE skill ALTER COLUMN workspace_id DROP NOT NULL;

-- A global skill must have an owner so it can be surfaced to the right
-- workspaces and so only that owner can manage it. Workspace skills may keep a
-- NULL creator (legacy rows), so the CHECK only constrains the global case.
ALTER TABLE skill
    ADD CONSTRAINT skill_global_requires_owner
    CHECK (workspace_id IS NOT NULL OR created_by IS NOT NULL);

-- UNIQUE(workspace_id, name) does not guard global skills, because PostgreSQL
-- treats NULL workspace_id values as distinct. A partial unique index keeps a
-- single owner from holding two global skills with the same name (the daemon
-- materialises skills to disk by name, so duplicates would collide).
CREATE UNIQUE INDEX skill_global_owner_name_key
    ON skill (created_by, name)
    WHERE workspace_id IS NULL;

-- Listing a workspace's skills now also fetches global skills owned by its
-- members; index the owner lookup over the global subset.
CREATE INDEX idx_skill_global_owner
    ON skill (created_by)
    WHERE workspace_id IS NULL;
