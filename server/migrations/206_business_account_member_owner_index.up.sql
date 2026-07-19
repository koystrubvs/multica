CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS business_account_member_one_owner_uidx ON business_account_member (business_id) WHERE role = 'owner';
