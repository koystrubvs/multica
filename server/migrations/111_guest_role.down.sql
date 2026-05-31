-- Revert the 'guest' role. Any existing guest members/invitations would
-- violate the narrowed CHECK and block this down migration — intended: only
-- run when no guests remain.

ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member'));

ALTER TABLE workspace_invitation DROP CONSTRAINT workspace_invitation_role_check;
ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member'));
