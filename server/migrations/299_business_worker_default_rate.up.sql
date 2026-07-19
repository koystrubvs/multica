ALTER TABLE business_worker
    ADD COLUMN IF NOT EXISTS default_role text,
    ADD COLUMN IF NOT EXISTS default_percent numeric(7,4);
ALTER TABLE business_worker
    ADD CONSTRAINT business_worker_default_role_check CHECK (
        default_role IS NULL OR default_role IN ('pm', 'executor', 'reviewer', 'seo', 'content', 'copywriter', 'designer', 'domain_reviewer')
    );
ALTER TABLE business_worker
    ADD CONSTRAINT business_worker_default_percent_check CHECK (
        default_percent IS NULL OR (default_percent >= 0 AND default_percent <= 100)
    );
