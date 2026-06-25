import { useT } from "../i18n";
import { formatLastSeen, type RelativeTimeLabels } from "./utils";

// Localized wrapper around the pure formatLastSeen() compound formatter.
// Returns a stable-call-shape function so sites keep their terse usage:
//   const fmtLastSeen = useFormatLastSeen(); ...fmtLastSeen(runtime.last_seen_at)
// The pure formatter still defaults to English for non-React callers and
// the cost-math tests; this hook just feeds it the runtimes-namespace
// translations ("Just now" → "только что", "2m 14s ago" → "2м 14с назад").
export function useFormatLastSeen(): (lastSeenAt: string | null) => string {
  const { t } = useT("runtimes");
  const labels: RelativeTimeLabels = {
    never: t(($) => $.last_seen.never),
    justNow: t(($) => $.last_seen.just_now),
    ago: t(($) => $.last_seen.ago),
    unitSecond: t(($) => $.last_seen.unit_s),
    unitMinute: t(($) => $.last_seen.unit_m),
    unitHour: t(($) => $.last_seen.unit_h),
    unitDay: t(($) => $.last_seen.unit_d),
  };
  return (lastSeenAt: string | null) => formatLastSeen(lastSeenAt, labels);
}
