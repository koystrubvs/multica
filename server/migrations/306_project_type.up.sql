-- Classify client-facing website projects without coupling the workspace-level
-- project API to owner-only Business finance records.
ALTER TABLE project
    ADD COLUMN project_type TEXT;

ALTER TABLE project
    ADD CONSTRAINT project_project_type_check
    CHECK (project_type IN ('support', 'seo', 'development', 'transit'));

-- Reuse classifications that were already explicitly confirmed in Business.
-- "internal" is only translated when the project title itself identifies the
-- transit model; unrelated internal projects remain unclassified.
UPDATE project AS p
SET project_type = CASE
    WHEN bcp.service_type IN ('support', 'seo', 'development') THEN bcp.service_type
    WHEN bcp.service_type = 'internal' AND lower(p.title) LIKE '%транзит%' THEN 'transit'
    ELSE NULL
END
FROM business_client_project AS bcp
WHERE bcp.project_id = p.id
  AND p.project_type IS NULL
  AND (
      bcp.service_type IN ('support', 'seo', 'development')
      OR (bcp.service_type = 'internal' AND lower(p.title) LIKE '%транзит%')
  );
