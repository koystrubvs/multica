-- 310: per-property visibility.
--
-- The property catalog had no visibility of any kind: ListIssueProperties
-- returned every definition to every member, and values rode along in the
-- issue payload bag on every list, card and broadcast.
--
-- One mode is enough at this scale (a workspace caps at 20 active
-- definitions), so this is a mode column, not a per-role ACL:
--
--   workspace — everyone who can see the issue sees the property (default,
--               which is what every existing definition gets)
--   owner     — only billing staff (owner/admin) and agents
--
-- The immediate case is «Биллинг». It is not an amount, but it CONTROLS the
-- invoice: the sweep skips issues marked internal and period confirmation
-- voids charges already collected on them. Any member could take the
-- "internal" mark off a task and push their own work into a client invoice.
--
-- Agents must keep writing hidden properties — the workspace context tells
-- every agent to fill «Биллинг» when it closes a task, and a gate that
-- blocked them would silently start billing internal work to clients. The
-- handler condition is therefore "agent OR billing staff", never role alone.
ALTER TABLE issue_property
  ADD COLUMN visibility TEXT NOT NULL DEFAULT 'workspace';

ALTER TABLE issue_property
  ADD CONSTRAINT issue_property_visibility_check
  CHECK (visibility IN ('workspace', 'owner'));
