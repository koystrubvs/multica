ALTER TABLE business_recurring_cost
ADD COLUMN frequency TEXT NOT NULL DEFAULT 'monthly';

ALTER TABLE business_recurring_cost
ADD CONSTRAINT business_recurring_cost_frequency_check
CHECK (frequency IN ('monthly', 'yearly'));
