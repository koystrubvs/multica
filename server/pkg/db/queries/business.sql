-- W1 Business Control Plane read paths. Every user-facing query joins the
-- database membership row: business access is independent from workspace
-- membership and is never inferred from a cache or from owner_user_id alone.

-- name: ListBusinessAccountsForOwner :many
SELECT
    ba.id,
    ba.name,
    ba.owner_user_id,
    ba.currency,
    ba.timezone,
    ba.monthly_owner_income_target_rub::text AS monthly_owner_income_target_rub,
    bam.role,
    ba.created_at,
    ba.updated_at
FROM business_account_member bam
JOIN business_account ba ON ba.id = bam.business_id
WHERE bam.user_id = $1
  AND bam.role = 'owner'
  AND ba.owner_user_id = bam.user_id
ORDER BY ba.created_at ASC, ba.id ASC;

-- name: GetBusinessAccountForOwner :one
SELECT
    ba.id,
    ba.name,
    ba.owner_user_id,
    ba.currency,
    ba.timezone,
    ba.monthly_owner_income_target_rub::text AS monthly_owner_income_target_rub,
    bam.role,
    ba.created_at,
    ba.updated_at
FROM business_account_member bam
JOIN business_account ba ON ba.id = bam.business_id
WHERE ba.id = $1
  AND bam.user_id = $2
  AND bam.role = 'owner'
  AND ba.owner_user_id = bam.user_id;

-- name: GetBusinessAccountMember :one
SELECT business_id, user_id, role, created_at, updated_at
FROM business_account_member
WHERE business_id = $1 AND user_id = $2;

-- name: ListBusinessWorkspacesForOwner :many
SELECT
    bw.business_id,
    bw.workspace_id,
    w.name AS workspace_name,
    w.slug AS workspace_slug,
    bw.kind,
    bw.include_in_portfolio,
    bw.include_revenue,
    bw.include_costs,
    bw.client_id,
    bw.created_at,
    bw.updated_at
FROM business_account_member bam
JOIN business_workspace bw ON bw.business_id = bam.business_id
JOIN workspace w ON w.id = bw.workspace_id
WHERE bam.business_id = $1
  AND bam.user_id = $2
  AND bam.role = 'owner'
ORDER BY
    CASE bw.kind
        WHEN 'operational' THEN 1
        WHEN 'internal' THEN 2
        WHEN 'client' THEN 3
        WHEN 'archive' THEN 4
    END,
    w.name ASC,
    bw.workspace_id ASC;
