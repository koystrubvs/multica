import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  usage: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", rid, days, tz] as const,
  usageByAgent: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-agent", rid, days, tz] as const,
  // by-hour now follows the viewer's tz, like the other reports.
  usageByHour: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-hour", rid, days, tz] as const,
};

// `tz` is the viewer's IANA name — all reports follow the viewer's tz.
export function runtimeUsageOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usage(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsage(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByAgentOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usageByAgent(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByAgent(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByHourOptions(runtimeId: string, days: number, tz: string) {
  return queryOptions({
    queryKey: runtimeKeys.usageByHour(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByHour(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeListOptions(wsId: string, owner?: "me") {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }),
  });
}

// Daily USD->RUB rates (Bank of Russia) for [fromIso, toIso]. Rates are a
// daily fact, so an hour of staleness is fine; the key omits workspace because
// the data is workspace-independent (workspace_id only satisfies route auth).
export function fxDailyOptions(
  fromIso: string,
  toIso: string,
  workspaceId: string,
) {
  return queryOptions({
    queryKey: ["fx", "daily", fromIso, toIso] as const,
    queryFn: () =>
      api.getFxDaily({ workspace_id: workspaceId, from: fromIso, to: toIso }),
    staleTime: 60 * 60 * 1000,
  });
}
