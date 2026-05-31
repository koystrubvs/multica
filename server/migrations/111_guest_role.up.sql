-- P10: SitePing client "guest" role.
--
-- Guests are real workspace members with a deliberately narrow surface: they
-- can view the board and comment on issues, but cannot assign work to agents
-- or squads (enforced in handler.validateAssigneePair) and are excluded from
-- every owner/admin/member-gated endpoint by virtue of not being in those
-- role lists. This migration only widens the role CHECK constraints so the
-- 'guest' value is storable on members and invitations.

ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member', 'guest'));

ALTER TABLE workspace_invitation DROP CONSTRAINT workspace_invitation_role_check;
ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member', 'guest'));
