-- 315: the access mode, plus the guest_project backfill.
--
-- 'workspace' — sees every project in the workspace. Default, and what every
--               existing member gets: this migration changes no one's access.
-- 'projects'  — sees only what member_project grants.
--
-- Guests are scoped by their bindings regardless of this column: their gate
-- keys off role = 'guest', which predates the column and stays as it is.
ALTER TABLE member
  ADD COLUMN IF NOT EXISTS access_scope TEXT NOT NULL DEFAULT 'workspace';

ALTER TABLE member
  ADD CONSTRAINT member_access_scope_check CHECK (access_scope IN ('workspace', 'projects'));

-- Backfill from guest_project so the new table is authoritative from the first
-- request. guest_project is left in place, unused, as the rollback path.
INSERT INTO member_project (workspace_id, user_id, project_id, created_at)
SELECT g.workspace_id, g.user_id, g.project_id, g.created_at
  FROM guest_project g
ON CONFLICT (user_id, project_id) DO NOTHING;
