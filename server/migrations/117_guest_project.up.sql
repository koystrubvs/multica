-- 117: P10 guest <-> project scoping (Variant A, widget-only).
--
-- A guest member (SitePing client) is bound to one or more projects. The issue
-- create / read / comment paths reject any guest request whose target issue
-- does not belong to a bound project. Without this a guest PAT could file into,
-- read, or comment on ANY project in the workspace. Non-guest roles are
-- unaffected (the gate only triggers for role = guest).
CREATE TABLE IF NOT EXISTS guest_project (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, project_id)
);
CREATE INDEX IF NOT EXISTS idx_guest_project_user ON guest_project (user_id, workspace_id);
CREATE INDEX IF NOT EXISTS idx_guest_project_project ON guest_project (project_id);
