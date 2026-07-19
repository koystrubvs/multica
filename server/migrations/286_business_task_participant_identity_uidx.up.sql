CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_task_participant_identity_uidx ON business_task_participant (task_economics_id, worker_id, role);
