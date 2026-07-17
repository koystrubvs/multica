-- FORK-LOCAL (koystrub.dev self-host): Human Attribution phase-2 backfill.
--
-- Upstream migrations 197_agent_task_attribution_strict_constraint (ADD ...
-- NOT VALID) and 198_..._validate (VALIDATE) assume the legacy rows that
-- migration 190 temporarily exempted (originator_source IS NULL) were already
-- "backfilled out of band" before 198 runs. That out-of-band backfill was an
-- upstream deploy-ops step and never happened on this self-host instance, so
-- 198's VALIDATE would fail closed on our historical agent_task_queue rows
-- (terminal tasks written between attribution rollout and the accountable-user
-- wiring: originator_user_id set, accountable_user_id NULL, source NULL).
--
-- This file performs exactly the repair upstream's own regression test does
-- (server/internal/migrations/attribution_constraint_migration_test.go):
--   SET accountable_user_id = originator_user_id, originator_source = 'backfill'
-- The accountable person for a legacy originated task IS its originator.
--
-- Filename sorts lexicographically AFTER
-- "197_agent_task_attribution_strict_constraint" ('g' < 't') and BEFORE
-- "198_...", so the migrate runner (sort.Strings) applies it in the gap
-- between the strict constraint's NOT VALID ADD and its full-table VALIDATE.
-- The UPDATE satisfies both the strict shadow constraint and migration 190's
-- transitional constraint, so it runs cleanly under either. Idempotent: after
-- it runs no row matches its WHERE, so a re-run (renumber drift) is a no-op.
UPDATE agent_task_queue
SET accountable_user_id = originator_user_id,
    originator_source   = 'backfill'
WHERE originator_source IS NULL
  AND originator_user_id IS NOT NULL
  AND (accountable_user_id IS NULL OR accountable_user_id <> originator_user_id);
