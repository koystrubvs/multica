-- 311: per-member project scope — the single binding table for every role.
--
-- Generalizes guest_project (migration 117). That table bound SitePing guests
-- to the projects they may touch; the same fact is now needed for employees,
-- and keeping two tables would mean two predicates, two admin surfaces and two
-- places to forget. guest_project is backfilled into this one in migration 315
-- and left in place, unused, so the change is reversible.
--
-- No foreign keys by repository rule: dependent cleanup is explicit in the
-- handler (member removal and role change prune bindings in the same
-- transaction as the member row).
--
-- Access is a MODE on the member, not the presence of rows here:
-- member.access_scope = 'workspace' means "sees everything" and no rows are
-- needed at all, which is the common case for staff. Rows only matter for
-- access_scope = 'projects' and for guests.
CREATE TABLE IF NOT EXISTS member_project (
    id           UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    user_id      UUID NOT NULL,
    project_id   UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   UUID
);
