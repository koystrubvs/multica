-- Per-member project scope (migration 311). One table for every role: guests
-- were bound through guest_project before, employees need the same fact, and
-- two tables would have meant two predicates and two admin surfaces.

-- name: CreateMemberProject :one
INSERT INTO member_project (workspace_id, user_id, project_id, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, project_id) DO UPDATE SET project_id = EXCLUDED.project_id
RETURNING *;

-- name: DeleteMemberProject :exec
DELETE FROM member_project WHERE user_id = $1 AND project_id = $2;

-- name: DeleteMemberProjectsByUser :exec
DELETE FROM member_project WHERE user_id = $1 AND workspace_id = $2;

-- name: ListMemberProjectsByUser :many
SELECT project_id FROM member_project
WHERE user_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListMemberProjectsByWorkspace :many
-- The whole workspace's bindings in one read, for the members screen.
SELECT user_id, project_id FROM member_project
WHERE workspace_id = $1
ORDER BY user_id, created_at ASC;

-- name: ListMemberUserIDsByProject :many
SELECT user_id FROM member_project
WHERE project_id = $1;

-- name: MemberHasProjectAccess :one
SELECT EXISTS(
    SELECT 1 FROM member_project WHERE user_id = $1 AND project_id = $2
) AS has_access;

-- name: CountIssuesAssignedToMemberInProject :one
-- How many issues would be orphaned by revoking this person's access to this
-- project. Drives the "who takes these over?" prompt.
SELECT COUNT(*) FROM issue
WHERE workspace_id = $1 AND project_id = $2
  AND assignee_type = 'member' AND assignee_id = $3;

-- name: ReassignIssuesInProject :execrows
-- Hands this person's issues in one project to someone else, or clears the
-- assignee when new_assignee_id is NULL. Runs in the same transaction as the
-- binding delete: between the two the issue would otherwise sit with an
-- assignee who can no longer see it.
UPDATE issue
SET assignee_type = CASE WHEN sqlc.narg('new_assignee_id')::uuid IS NULL THEN NULL ELSE 'member' END,
    assignee_id   = sqlc.narg('new_assignee_id')::uuid,
    updated_at    = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND project_id = sqlc.arg('project_id')::uuid
  AND assignee_type = 'member'
  AND assignee_id = sqlc.arg('from_user_id')::uuid;

-- name: ReassignAllIssuesOfMember :execrows
-- Same, for every project at once — used when a member is removed outright.
UPDATE issue
SET assignee_type = CASE WHEN sqlc.narg('new_assignee_id')::uuid IS NULL THEN NULL ELSE 'member' END,
    assignee_id   = sqlc.narg('new_assignee_id')::uuid,
    updated_at    = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND assignee_type = 'member'
  AND assignee_id = sqlc.arg('from_user_id')::uuid;
