-- No-op: data backfills are not reversed. We cannot distinguish rows whose
-- accountable_user_id/originator_source this migration set from rows that
-- legitimately carry the same values, so rolling back would risk clobbering
-- good attribution data. Rolling back the strict constraint (upstream 199/198/
-- 197 down) is sufficient to restore the pre-phase-2 enforcement shape.
SELECT 1;
