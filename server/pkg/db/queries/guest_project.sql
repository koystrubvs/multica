-- name: CreateGuestProject :one
INSERT INTO guest_project (workspace_id, user_id, project_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, project_id) DO UPDATE SET project_id = EXCLUDED.project_id
RETURNING *;

-- name: DeleteGuestProject :exec
DELETE FROM guest_project WHERE user_id = $1 AND project_id = $2;

-- name: DeleteGuestProjectsByUser :exec
DELETE FROM guest_project WHERE user_id = $1 AND workspace_id = $2;

-- name: ListGuestProjectsByUser :many
SELECT project_id FROM guest_project
WHERE user_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListGuestUserIDsByProject :many
SELECT user_id FROM guest_project
WHERE project_id = $1;

-- name: GuestHasProjectAccess :one
SELECT EXISTS(
    SELECT 1 FROM guest_project WHERE user_id = $1 AND project_id = $2
) AS has_access;
