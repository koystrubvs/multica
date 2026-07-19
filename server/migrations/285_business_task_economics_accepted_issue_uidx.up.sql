CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_task_economics_accepted_issue_uidx ON business_task_economics (issue_id) WHERE status = 'accepted';
