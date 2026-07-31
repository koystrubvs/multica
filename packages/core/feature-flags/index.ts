/**
 * Public surface for @multica/core/feature-flags.
 *
 * Keep this list minimal — every new export becomes a contract we have to
 * preserve across the monorepo. Add to it only when a real caller appears.
 */

export type {
  Decision,
  EvalContext,
  PercentRollout,
  Provider,
  Reason,
  Rule,
} from "./types";

export { FeatureFlagService } from "./service";
export { StaticProvider } from "./static-provider";
export { ChainProvider } from "./chain-provider";
export {
  COMPOSIO_MCP_APPS_FLAG,
  BUSINESS_CONTROL_PLANE_FLAG,
  BUSINESS_DASHBOARD_FLAG,
  BUSINESS_CLIENTS_UI_FLAG,
  BUSINESS_CALENDAR_FLAG,
  BUSINESS_BANK_IMPORT_FLAG,
  BUSINESS_TASK_ECONOMICS_SHADOW_FLAG,
  BUSINESS_TASK_ECONOMICS_ACCEPT_FLAG,
  BUSINESS_ACCRUALS_FLAG,
  BUSINESS_PAYOUT_BATCHES_FLAG,
  MODULBANK_PAYOUT_DRAFTS_FLAG,
} from "./keys";
export {
  FeatureFlagsProvider,
  useFeatureFlagService,
  useFlag,
  useVariant,
} from "./context";

// Hash helpers are exported for tests and for callers that want to share
// the bucketing logic without going through a Provider (rare; usually a
// red flag that the caller should be using the Service instead).
export { bucketFor, inPercent } from "./hash";
