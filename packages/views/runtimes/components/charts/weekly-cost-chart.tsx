import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Cell,
} from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { WeeklyCostStackData } from "../../utils";
import { useT } from "../../../i18n";
import { formatRub } from "../../utils";

// Same three-segment stack as DailyCostChart — keeping series, colours, and
// ordering identical so the user reads "Weekly" as a coarser cut of the same
// chart, not a different chart. Partial-week bars render at half-opacity so
// "this week is in progress" is visually obvious without a separate legend.
//
// Values arrive already in rubles (per-date historical CBR conversion happens
// in `aggregateByWeek` upstream), so `formatRub` here just formats.
export const weeklyCostStackConfig = {
  input: { label: "Input", color: "var(--chart-1)" },
  output: { label: "Output", color: "var(--chart-2)" },
  cacheWrite: { label: "Cache write", color: "var(--chart-3)" },
} satisfies ChartConfig;

export function WeeklyCostChart({ data }: { data: WeeklyCostStackData[] }) {
  const { t } = useT("runtimes");
  // Localize the tooltip series labels: recharts passes the raw dataKey as
  // `name`, not the config label, so without this the tooltip showed English
  // while the legend was translated.
  const seriesLabel = (name: unknown): string => {
    switch (name) {
      case "input":
        return t(($) => $.usage.legend_input);
      case "output":
        return t(($) => $.usage.legend_output);
      case "cacheRead":
        return t(($) => $.usage.legend_cache_read);
      case "cacheWrite":
        return t(($) => $.usage.legend_cache_write);
      default:
        return String(name);
    }
  };
  return (
    <ChartContainer config={weeklyCostStackConfig} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: number) => formatRub(v)}
          width={64}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelKey="rangeLabel"
              labelFormatter={(_label, payload) => {
                const row = payload[0]?.payload as WeeklyCostStackData | undefined;
                if (!row) return "";
                return row.partial
                  ? t(($) => $.usage.weekly_partial_label, {
                      range: row.rangeLabel,
                      covered: row.daysCovered,
                    })
                  : row.rangeLabel;
              }}
              formatter={(value, name) =>
                typeof value === "number"
                  ? `${formatRub(value)} ${seriesLabel(name)}`
                  : `${value} ${seriesLabel(name)}`
              }
              footer={(payload) => {
                const total = payload.reduce(
                  (sum, item) =>
                    sum + (typeof item.value === "number" ? item.value : 0),
                  0,
                );
                return (
                  <div className="flex items-center justify-between gap-2 font-medium">
                    <span>{t(($) => $.charts.tooltip_total)}</span>
                    <span className="font-mono tabular-nums">
                      {formatRub(total)}
                    </span>
                  </div>
                );
              }}
            />
          }
        />
        <Bar dataKey="input" stackId="cost" fill="var(--color-input)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar dataKey="output" stackId="cost" fill="var(--color-output)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar
          dataKey="cacheWrite"
          stackId="cost"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        >
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}
