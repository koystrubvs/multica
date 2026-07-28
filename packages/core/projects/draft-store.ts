import type { ProjectStatus, ProjectPriority, ProjectType } from "../types";
import { createDraftStore } from "../drafts/create-draft-store";

interface ProjectDraft {
  title: string;
  description: string;
  status: ProjectStatus;
  priority: ProjectPriority;
  projectType?: ProjectType;
  leadType?: "member" | "agent";
  leadId?: string;
  icon?: string;
  // Calendar days ("YYYY-MM-DD"); empty/undefined means unset.
  startDate?: string;
  dueDate?: string;
}

const EMPTY_DRAFT: ProjectDraft = {
  title: "",
  description: "",
  status: "planned",
  priority: "none",
  projectType: undefined,
  leadType: undefined,
  leadId: undefined,
  icon: undefined,
  startDate: undefined,
  dueDate: undefined,
};

export const useProjectDraftStore = createDraftStore<ProjectDraft>({
  storageKey: "multica_project_draft",
  emptyData: EMPTY_DRAFT,
  hasMeaningful: (d) => !!(d.title || d.description),
});
