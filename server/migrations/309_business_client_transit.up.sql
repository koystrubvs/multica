-- Some clients ask us to invoice more than we worked and pass the difference
-- back to them, keeping a commission: the invoice is 350 000 where the work
-- was 70 000, and 10% of the 280 000 pass-through is ours. Money that only
-- travels through the account is not revenue, so the two rates live on the
-- client and NULL means "not a pass-through client at all" (0 is a real value:
-- one client hands the whole difference on and keeps nothing).

ALTER TABLE business_client
ADD COLUMN transit_commission_percent NUMERIC(5,2),
ADD COLUMN transit_tax_percent NUMERIC(5,2);

ALTER TABLE business_client
ADD CONSTRAINT business_client_transit_commission_check
CHECK (transit_commission_percent IS NULL OR (transit_commission_percent >= 0 AND transit_commission_percent <= 100));

ALTER TABLE business_client
ADD CONSTRAINT business_client_transit_tax_check
CHECK (transit_tax_percent IS NULL OR (transit_tax_percent >= 0 AND transit_tax_percent <= transit_commission_percent));
