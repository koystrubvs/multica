"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import type { ProjectStatus } from "../../types";

export type ProjectViewMode = "compact" | "comfortable";

export interface ProjectViewState {
  viewMode: ProjectViewMode;
  setViewMode: (mode: ProjectViewMode) => void;
  statusFilter: ProjectStatus[];
  setStatusFilter: (next: ProjectStatus[]) => void;
}

export const useProjectViewStore = create<ProjectViewState>()(
  persist(
    (set) => ({
      viewMode: "compact",
      setViewMode: (mode) => set({ viewMode: mode }),
      statusFilter: [],
      setStatusFilter: (next) => set({ statusFilter: next }),
    }),
    {
      name: "multica_projects_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ viewMode: state.viewMode, statusFilter: state.statusFilter }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, viewMode: "compact", statusFilter: [] };
        return { ...current, ...(persisted as Partial<ProjectViewState>) };
      },
    }
  )
);

registerForWorkspaceRehydration(() => useProjectViewStore.persist.rehydrate());