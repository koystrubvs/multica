-- Per-agent Docker isolation. When true, the agent daemon runs this agent in an
-- ephemeral container (multica-agent-sandbox) instead of as a host child process.
-- The flag is surfaced in the UI as a first-class toggle; the backend mirrors it
-- into the agent custom_env (AGENT_ISOLATE) so the gate wrapper picks it up.
ALTER TABLE agent ADD COLUMN isolated BOOLEAN NOT NULL DEFAULT false;
