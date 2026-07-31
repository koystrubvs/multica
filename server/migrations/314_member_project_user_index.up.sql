-- One binding per (user, project); also the lookup index for "what may this
-- person see", which the scope resolver reads once per request.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS member_project_user_project_uidx ON member_project (user_id, project_id);
