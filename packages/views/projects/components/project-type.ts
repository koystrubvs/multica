import type { ProjectType } from "@multica/core/types";
import { useT } from "../../i18n";

export const PROJECT_TYPE_ORDER: ProjectType[] = [
  "support",
  "seo",
  "development",
  "transit",
];

export function useProjectTypeLabels(): Record<ProjectType, string> {
  const { t } = useT("projects");
  return {
    support: t(($) => $.type.support),
    seo: t(($) => $.type.seo),
    development: t(($) => $.type.development),
    transit: t(($) => $.type.transit),
  };
}
