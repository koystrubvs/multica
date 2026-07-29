"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useFeatureEnabled } from "@multica/core/config";
import {
  BUSINESS_ACCRUALS_FLAG,
  BUSINESS_BANK_IMPORT_FLAG,
  BUSINESS_CALENDAR_FLAG,
  BUSINESS_CLIENTS_UI_FLAG,
  BUSINESS_CONTROL_PLANE_FLAG,
  BUSINESS_DASHBOARD_FLAG,
  BUSINESS_PAYOUT_BATCHES_FLAG,
  BUSINESS_TASK_ECONOMICS_ACCEPT_FLAG,
  BUSINESS_TASK_ECONOMICS_SHADOW_FLAG,
  MODULBANK_PAYOUT_DRAFTS_FLAG,
} from "@multica/core/feature-flags";
import type { BusinessRow } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { AlertTriangle, Building2, ChevronLeft, ChevronRight, CircleDollarSign, Filter, HandCoins, Landmark, LayoutDashboard, ListChecks, Plus, ReceiptText, Search, Users, Wallet, X } from "lucide-react";
import { FILTER_ITEM_CLASS, HoverCheck } from "../common/hover-check";
import { useT } from "../i18n";
import { PageHeader } from "../layout/page-header";
import { BusinessBillingTab } from "./business-billing-tab";
import { BusinessCapMeter, hasCapMeter } from "./business-cap-meter";
import { BusinessClientCard } from "./business-client-card";
import { BusinessClientCreateSheet } from "./business-client-create";
import { BusinessCostsTab } from "./business-costs-tab";
import { BusinessReceivablePaymentSheet } from "./business-receivable-payment";

type Tab = "overview" | "calendar" | "clients" | "billing" | "bank" | "costs" | "economics" | "team";

const TAB_ICONS: Record<Tab, React.ComponentType<{ className?: string }>> = {
  overview: LayoutDashboard,
  calendar: Wallet,
  clients: Users,
  billing: ReceiptText,
  bank: Landmark,
  costs: CircleDollarSign,
  economics: ListChecks,
  team: HandCoins,
};

// Same trigger styling as the settings page sidebar so both surfaces read as
// one navigation pattern: horizontal line-tabs on mobile, icon list on desktop.
const NAV_TAB_TRIGGER_CLASS =
  "h-8 shrink-0 px-2.5 hover:bg-surface-hover data-active:!bg-surface-selected data-active:!text-surface-selected-foreground data-active:hover:!bg-surface-selected md:!w-full md:px-2 md:after:hidden";

const SERVICE_TYPES = ["development", "support", "seo", "content"] as const;
const CLASSIFICATIONS = ["client_income", "payroll", "tax", "service", "transfer", "owner_draw", "vitmax_transit", "unknown"] as const;
const WORKER_ROLES = ["executor", "pm", "reviewer", "seo", "content", "copywriter", "designer", "domain_reviewer"] as const;
const CLIENT_STATUSES = ["active", "prospect", "paused", "leaving", "lost"] as const;
const RECEIVABLE_STATUSES = ["expected", "invoiced", "partially_paid", "paid", "skipped", "written_off"] as const;
const COUNTERPARTY_CLASSES = ["client_payer", "worker_payee", "vendor", "transit", "ignored", "unresolved"] as const;
const COUNTERPARTY_REASON_KEYS: Record<string, string> = {
  "Owner-approved personal business payer registry": "bank.reasons.owner_approved_personal_payer_registry",
  "manual bank counterparty resolution": "bank.reasons.manual_counterparty_resolution",
  "VitMax transit; excluded from personal revenue": "bank.reasons.vitmax_transit_excluded",
  "Own-account transfer; excluded from revenue and expenses": "bank.reasons.own_account_transfer_excluded",
};

type TT = (key: string, options?: { defaultValue?: string }) => string;

type ColumnKind = "text" | "money" | "date" | "datetime" | "bool" | "enum" | "percent" | "receivable_status";
interface ColumnSpec {
  key: string;
  kind?: ColumnKind;
}

function normalizeColumn(column: string | ColumnSpec): ColumnSpec {
  return typeof column === "string" ? { key: column } : column;
}

function isTruthyFlag(value: unknown): boolean {
  return value === true || value === "t" || value === "true";
}

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function rub(value: string | number | undefined): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

function receivableRemaining(row: BusinessRow): number {
  return Math.max(Number(row.planned_amount_rub ?? 0) - Number(row.paid_amount_rub ?? 0), 0);
}

function receivableAcceptsPayment(row: BusinessRow): boolean {
  return !["paid", "skipped", "written_off"].includes(String(row.status ?? "")) && receivableRemaining(row) > 0;
}

export function groupUnresolvedBankCounterparties(rows: BusinessRow[]): BusinessRow[] {
  const groups = new Map<string, {
    transaction_id: string;
    inbound_transaction_id: string;
    outbound_transaction_id: string;
    counterparty_name: string;
    counterparty_inn: string;
    operation_count: number;
    inbound_rub: number;
    outbound_rub: number;
  }>();
  for (const row of rows) {
    if (String(row.classification ?? "") !== "unknown") continue;
    const name = String(row.counterparty_name ?? "").trim();
    const inn = String(row.counterparty_inn ?? "").trim();
    const key = inn ? `inn:${inn}` : `name:${name.toLocaleLowerCase().replaceAll(/\s+/g, " ")}`;
    const entry = groups.get(key) ?? {
      transaction_id: String(row.id ?? ""),
      inbound_transaction_id: "",
      outbound_transaction_id: "",
      counterparty_name: name,
      counterparty_inn: inn,
      operation_count: 0,
      inbound_rub: 0,
      outbound_rub: 0,
    };
    entry.operation_count += 1;
    if (String(row.direction) === "inbound") {
      entry.inbound_transaction_id ||= String(row.id ?? "");
      entry.inbound_rub += Number(row.amount_rub ?? 0);
    } else {
      entry.outbound_transaction_id ||= String(row.id ?? "");
      entry.outbound_rub += Number(row.amount_rub ?? 0);
    }
    groups.set(key, entry);
  }
  return [...groups.values()].sort((a, b) => (b.inbound_rub + b.outbound_rub) - (a.inbound_rub + a.outbound_rub));
}

export function parseCounterpartyResolutionTarget(value: string): {
  classification: "client_payer" | "worker_payee" | "vendor" | "transit" | "ignored";
  client_id?: string;
  worker_id?: string;
} | null {
  const separator = value.indexOf(":");
  if (separator < 1) return null;
  const type = value.slice(0, separator);
  const id = value.slice(separator + 1);
  if (type === "client" && id) return { classification: "client_payer", client_id: id };
  if (type === "worker" && id) return { classification: "worker_payee", worker_id: id };
  if (type === "class" && ["vendor", "transit", "ignored"].includes(id)) {
    return { classification: id as "vendor" | "transit" | "ignored" };
  }
  return null;
}

export function counterpartyReasonTranslationKey(reason: string): string | null {
  return COUNTERPARTY_REASON_KEYS[reason] ?? null;
}

export function counterpartyResolutionTransactionID(
  row: BusinessRow,
  resolution: NonNullable<ReturnType<typeof parseCounterpartyResolutionTarget>>,
): string {
  if (resolution.classification === "client_payer") return String(row.inbound_transaction_id ?? "");
  if (resolution.classification === "worker_payee") return String(row.outbound_transaction_id ?? "");
  return String(row.transaction_id ?? "");
}

const PILL_BAD = new Set(["overdue", "failed", "lost", "written_off", "void", "voided", "leaving", "inactive", "escalated", "missed"]);
const PILL_GOOD = new Set(["paid", "active", "accepted", "confirmed", "reconciled", "completed", "client_income"]);

function StatusPill({ value, label }: { value: string; label: string }) {
  const tone = PILL_BAD.has(value)
    ? "bg-destructive/10 text-destructive"
    : PILL_GOOD.has(value)
      ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
      : "bg-muted text-muted-foreground";
  return <span className={cn("inline-flex shrink-0 items-center whitespace-nowrap rounded px-1.5 py-0.5 text-[11px] font-medium", tone)}>{label}</span>;
}

function ReviewHint({ details, tt }: { details: string; tt: TT }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span aria-label={tt("columns.needs_review", { defaultValue: "review" })} />}
        className="inline-flex cursor-help items-center text-amber-600 dark:text-amber-400"
      >
        <AlertTriangle className="size-3" />
      </TooltipTrigger>
      <TooltipContent className="max-w-64">
        {details
          ? `${tt("columns.review_details", { defaultValue: "review" })}: ${details}`
          : tt("columns.needs_review", { defaultValue: "review" })}
      </TooltipContent>
    </Tooltip>
  );
}

function cellNode(row: BusinessRow, spec: ColumnSpec, tt: TT, locale: string): React.ReactNode {
  const value = row[spec.key];
  if (value === null || value === undefined || value === "") return <span className="text-muted-foreground/50">—</span>;
  switch (spec.kind) {
    case "money": {
      const amount = Number(value);
      if (!Number.isFinite(amount) || amount === 0) return <span className="text-muted-foreground/50">—</span>;
      return <span className="tabular-nums">{rub(String(value))}</span>;
    }
    case "date": {
      const parsed = new Date(String(value));
      return <span className="tabular-nums text-muted-foreground">{Number.isNaN(parsed.getTime()) ? String(value) : parsed.toLocaleDateString(locale)}</span>;
    }
    case "datetime": {
      const parsed = new Date(String(value));
      return <span className="tabular-nums text-muted-foreground">{Number.isNaN(parsed.getTime()) ? String(value) : parsed.toLocaleString(locale, { dateStyle: "short", timeStyle: "short" })}</span>;
    }
    case "bool":
      // eslint-disable-next-line i18next/no-literal-string -- compact boolean glyph, not user-facing copy
      return isTruthyFlag(value) ? <span>✓</span> : <span className="text-muted-foreground/50">—</span>;
    case "enum": {
      const raw = String(value);
      return <StatusPill value={raw} label={tt(`values.${raw}`, { defaultValue: raw })} />;
    }
    case "receivable_status": {
      const raw = String(value);
      const overdue = isTruthyFlag(row.is_overdue) && !["paid", "skipped", "written_off"].includes(raw);
      const pillValue = overdue ? "overdue" : raw;
      return (
        <span className="inline-flex items-center gap-1">
          <StatusPill value={pillValue} label={tt(`values.${pillValue}`, { defaultValue: pillValue })} />
          {isTruthyFlag(row.needs_review) && <ReviewHint details={String(row.review_details ?? "")} tt={tt} />}
        </span>
      );
    }
    case "percent":
      return <span className="tabular-nums">{Number(value)}%</span>;
    default: {
      const rendered = text(row, spec.key);
      const reasonKey = spec.key === "reason" ? counterpartyReasonTranslationKey(rendered) : null;
      const localized = reasonKey ? tt(reasonKey, { defaultValue: rendered }) : rendered;
      return <span className="truncate" title={localized}>{localized}</span>;
    }
  }
}

function RowTable({ rows, columns, empty, tt, locale, extra, onRowClick }: {
  rows: BusinessRow[];
  columns: (string | ColumnSpec)[];
  empty: string;
  tt: TT;
  locale: string;
  extra?: { header: string; render: (row: BusinessRow) => React.ReactNode };
  onRowClick?: (row: BusinessRow) => void;
}) {
  const specs = columns.map(normalizeColumn);
  if (rows.length === 0) return <div className="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">{empty}</div>;
  return (
    <div className="rounded-lg border">
      <Table data-testid="business-row-table" containerClassName="max-h-[60vh] overflow-auto">
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            {specs.map((spec) => (
              <TableHead key={spec.key} className="sticky top-0 z-10 h-8 bg-background text-xs font-medium text-muted-foreground">
                {tt(`columns.${spec.key}`, { defaultValue: spec.key.replaceAll("_", " ") })}
              </TableHead>
            ))}
            {extra && <TableHead className="sticky top-0 z-10 h-8 bg-background text-xs font-medium text-muted-foreground">{extra.header}</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.slice(0, 200).map((row, index) => (
            <TableRow key={String(row.id ?? index)} className={cn(onRowClick && "cursor-pointer")} onClick={onRowClick ? () => onRowClick(row) : undefined}>
              {specs.map((spec, specIndex) => (
                <TableCell key={spec.key} className={cn("max-w-[300px] truncate py-1.5 text-xs", specIndex === 0 && "font-medium")}>
                  {cellNode(row, spec, tt, locale)}
                </TableCell>
              ))}
              {extra && <TableCell className="py-1 text-xs">{extra.render(row)}</TableCell>}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

interface FilterSection {
  key: string;
  label: string;
  options: { value: string; label: string }[];
  selected: string[];
  onToggle: (value: string) => void;
}

interface FilterToggle {
  key: string;
  label: string;
  checked: boolean;
  defaultChecked?: boolean;
  onToggle: () => void;
}

function FilterMenu({ label, clearLabel, sections, toggles, onClear }: {
  label: string;
  clearLabel: string;
  sections: FilterSection[];
  toggles?: FilterToggle[];
  onClear: () => void;
}) {
  const activeCount = sections.reduce((sum, section) => sum + section.selected.length, 0)
    + (toggles ?? []).filter((toggle) => toggle.checked !== (toggle.defaultChecked ?? false)).length;
  const hasActive = activeCount > 0;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={hasActive ? "default" : "outline"}
            size="sm"
            aria-label={label}
            className={hasActive
              ? "h-8 w-8 gap-1 bg-brand px-0 text-white hover:bg-brand/90 md:w-auto md:px-2.5"
              : "h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"}
          >
            <Filter className="size-3.5" />
            <span className="hidden md:inline">{label}</span>
            {hasActive && <span className="tabular-nums">{activeCount}</span>}
            {hasActive && (
              <span
                role="button"
                tabIndex={-1}
                aria-label={clearLabel}
                className="-mr-1 ml-0.5 hidden rounded-sm p-0.5 hover:bg-white/20 md:inline-flex"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  onClear();
                }}
                onPointerDown={(event) => event.stopPropagation()}
              >
                <X className="size-3" />
              </span>
            )}
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-auto">
        {sections.map((section) => (
          <DropdownMenuSub key={section.key}>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{section.label}</span>
              {section.selected.length > 0 && (
                <span className="text-xs font-medium text-primary">{section.selected.length}</span>
              )}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-44 overflow-y-auto">
              {section.options.map((option) => (
                <DropdownMenuCheckboxItem
                  key={option.value}
                  checked={section.selected.includes(option.value)}
                  onCheckedChange={() => section.onToggle(option.value)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={section.selected.includes(option.value)} />
                  <span className="min-w-0 truncate">{option.label}</span>
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        ))}
        {(toggles ?? []).map((toggle) => (
          <DropdownMenuCheckboxItem
            key={toggle.key}
            checked={toggle.checked}
            onCheckedChange={toggle.onToggle}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={toggle.checked} />
            {toggle.label}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function Toolbar({ children }: { children: React.ReactNode }) {
  return <div className="flex min-h-10 flex-wrap items-center justify-between gap-2">{children}</div>;
}

function ResultCount({ shown, total }: { shown: number; total: number }) {
  if (shown === total) return null;
  return <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{shown} / {total}</span>;
}

function Section({ title, actions, children }: { title: string; actions?: React.ReactNode; children: React.ReactNode }) {
  return <section className="space-y-2"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</h2>{actions}</div>{children}</section>;
}

function Metric({ label, value, hint, warning, onClick }: { label: string; value: string; hint?: string; warning?: boolean; onClick?: () => void }) {
  const className = cn(
    "flex min-w-0 flex-col gap-1 rounded-lg border bg-card p-3 text-left",
    warning && "border-warning/50 bg-warning/5",
    onClick && "cursor-pointer transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
  );
  const content = <><div className="truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground" title={label}>{label}</div><div className="break-words text-base font-semibold leading-tight tabular-nums @3xl:text-lg">{value}</div>{hint && <div className="text-[11px] text-muted-foreground">{hint}</div>}</>;
  return onClick ? <button type="button" className={className} onClick={onClick}>{content}</button> : <div className={className}>{content}</div>;
}

function formData(event: FormEvent<HTMLFormElement>): FormData {
  event.preventDefault();
  return new FormData(event.currentTarget);
}

function monthOf(value: unknown): string {
  return String(value ?? "").slice(0, 7);
}

function monthLabel(monthKey: string, locale: string): string {
  const parsed = new Date(`${monthKey}-01T00:00:00`);
  if (Number.isNaN(parsed.getTime())) return monthKey;
  const label = parsed.toLocaleDateString(locale, { month: "long", year: "numeric" });
  return label.charAt(0).toUpperCase() + label.slice(1);
}

function toggleValue(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value];
}

export function BusinessPage() {
  const { t, i18n } = useT("business");
  const tt = t as unknown as TT;
  const locale = i18n?.language || "ru";
  const isMobile = useIsMobile();
  const controlPlaneEnabled = useFeatureEnabled(BUSINESS_CONTROL_PLANE_FLAG);
  const dashboardEnabled = useFeatureEnabled(BUSINESS_DASHBOARD_FLAG);
  const enabled = controlPlaneEnabled && dashboardEnabled;
  const clientsEnabled = useFeatureEnabled(BUSINESS_CLIENTS_UI_FLAG);
  const calendarEnabled = useFeatureEnabled(BUSINESS_CALENDAR_FLAG);
  const bankEnabled = useFeatureEnabled(BUSINESS_BANK_IMPORT_FLAG);
  const economicsEnabled = useFeatureEnabled(BUSINESS_TASK_ECONOMICS_SHADOW_FLAG);
  const acceptEnabled = useFeatureEnabled(BUSINESS_TASK_ECONOMICS_ACCEPT_FLAG);
  const accrualsEnabled = useFeatureEnabled(BUSINESS_ACCRUALS_FLAG);
  const payoutsEnabled = useFeatureEnabled(BUSINESS_PAYOUT_BATCHES_FLAG);
  const bankDraftsEnabled = useFeatureEnabled(MODULBANK_PAYOUT_DRAFTS_FLAG);
  const [selectedBusiness, setSelectedBusiness] = useState("");
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7));
  const [periodMode, setPeriodMode] = useState<"month" | "year">("month");
  const [tab, setTab] = useState<Tab>("overview");
  const [openClientID, setOpenClientID] = useState("");
  const [paymentReceivableID, setPaymentReceivableID] = useState("");
  const [createClientOpen, setCreateClientOpen] = useState(false);
  const [createCostOpen, setCreateCostOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const [scopeClient, setScopeClient] = useState<string[]>([]);
  const [scopeProject, setScopeProject] = useState<string[]>([]);
  const [scopeService, setScopeService] = useState<string[]>([]);

  const [clientStatuses, setClientStatuses] = useState<string[]>([]);
  const [counterpartyClasses, setCounterpartyClasses] = useState<string[]>([]);
  const [receivableStatuses, setReceivableStatuses] = useState<string[]>([]);
  const [receivableClients, setReceivableClients] = useState<string[]>([]);
  const [receivableReviewOnly, setReceivableReviewOnly] = useState(false);
  const [receivableOverdueOnly, setReceivableOverdueOnly] = useState(false);
  const [hideZeroReceivables, setHideZeroReceivables] = useState(true);
  const [txClasses, setTxClasses] = useState<string[]>([]);
  const [txDirections, setTxDirections] = useState<string[]>([]);
  const [txUnmatchedOnly, setTxUnmatchedOnly] = useState(false);
  const [txSearch, setTxSearch] = useState("");
  const [econWorker, setEconWorker] = useState("");
  const [econRole, setEconRole] = useState("");
  const [econPercent, setEconPercent] = useState("");
  const [accrualWorkers, setAccrualWorkers] = useState<string[]>([]);
  const [accrualStatuses, setAccrualStatuses] = useState<string[]>([]);
  const [econStatuses, setEconStatuses] = useState<string[]>([]);
  const [econClients, setEconClients] = useState<string[]>([]);
  const [payoutStatuses, setPayoutStatuses] = useState<string[]>([]);

  const accounts = useQuery({ queryKey: ["business", "accounts"], queryFn: () => api.listBusinessAccounts(), enabled });
  const businessID = selectedBusiness || accounts.data?.[0]?.id || "";
  const periodYear = month.slice(0, 4);
  const periodPrefix = periodMode === "year" ? periodYear : month;
  const dashboard = useQuery({
    queryKey: ["business", businessID, "dashboard", periodMode, month, scopeClient[0] ?? "", scopeProject[0] ?? "", scopeService[0] ?? ""],
    queryFn: () => api.getBusinessDashboard(businessID, periodMode === "year" ? { year: periodYear } : { month }, {
      client_id: scopeClient[0],
      project_id: scopeProject[0],
      service_type: scopeService[0],
    }),
    enabled: enabled && !!businessID,
  });
  const snapshot = useQuery({ queryKey: ["business", businessID, "snapshot"], queryFn: () => api.getBusinessSnapshot(businessID), enabled: enabled && !!businessID });
  const seriesGranularity = periodMode === "month" ? "day" : "month";
  const seriesFrom = periodMode === "month" ? `${month}-01` : `${periodYear}-01-01`;
  const seriesPeriods = periodMode === "month" ? new Date(Number(periodYear), Number(month.slice(5, 7)), 0).getDate() : 12;
  const series = useQuery({
    queryKey: ["business", businessID, "series", seriesFrom, seriesPeriods, seriesGranularity],
    queryFn: () => api.getBusinessDashboardSeries(businessID, seriesFrom, seriesPeriods, seriesGranularity),
    enabled: enabled && !!businessID,
  });

  const generatedRef = useRef(false);
  const bankImportInputRef = useRef<HTMLInputElement>(null);
  const snapshotRefetch = snapshot.refetch;
  useEffect(() => {
    if (tab !== "calendar" || !calendarEnabled || !businessID || !snapshot.data || generatedRef.current) return;
    generatedRef.current = true;
    void api.businessAction(businessID, "receivables/generate", { from_month: new Date().toISOString().slice(0, 7), months: 6 })
      .then(() => snapshotRefetch())
      .catch(() => {});
  }, [tab, calendarEnabled, businessID, snapshot.data, snapshotRefetch]);

  const execute = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key); setError(""); setMessage("");
    try {
      await action();
      setMessage(t(($) => $.success));
      await Promise.all([snapshot.refetch(), dashboard.refetch()]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy("");
    }
  };

  const data = snapshot.data;

  const filteredClients = useMemo(() => (data?.clients ?? []).filter((row) => clientStatuses.length === 0 || clientStatuses.includes(String(row.status))), [data?.clients, clientStatuses]);

  const filteredCounterparties = useMemo(() => (data?.counterparties ?? []).filter((row) => counterpartyClasses.length === 0 || counterpartyClasses.includes(String(row.classification))), [data?.counterparties, counterpartyClasses]);

  const filteredReceivables = useMemo(() => (data?.receivables ?? []).filter((row) =>
    (receivableStatuses.length === 0 || receivableStatuses.includes(String(row.status)))
    && (receivableClients.length === 0 || receivableClients.includes(String(row.client_id)))
    && (!receivableReviewOnly || isTruthyFlag(row.needs_review))
    && (!receivableOverdueOnly || isTruthyFlag(row.is_overdue))
  ), [data?.receivables, receivableStatuses, receivableClients, receivableReviewOnly, receivableOverdueOnly]);

  const agreementMeta = useMemo(() => {
    const map = new Map<string, { name: string; model: string; cap: number; channel: string; project: string }>();
    for (const row of data?.agreements ?? []) map.set(String(row.id), { name: String(row.name ?? ""), model: String(row.model ?? ""), cap: Number(row.cap_rub ?? 0), channel: String(row.payment_channel ?? ""), project: String(row.project_id ?? "") });
    return map;
  }, [data?.agreements]);

  // Every live ceiling, so the owner can see at a glance which client is
  // burning theirs faster than the period runs out.
  const cappedAgreements = useMemo(() => (data?.agreements ?? [])
    .filter((row) => String(row.status ?? "") === "active" && hasCapMeter(row))
    .sort((a, b) => String(a.client_name ?? "").localeCompare(String(b.client_name ?? "")) || String(a.name ?? "").localeCompare(String(b.name ?? ""))),
  [data?.agreements]);

  // Totals arrive per client *and* project. Both readings are kept: an
  // agreement pinned to a project must count only that project's work, while a
  // deal without one (two sites under a single contract) counts the client.
  const billingByClientMonth = useMemo(() => {
    const map = new Map<string, number>();
    const add = (key: string, amount: number) => map.set(key, (map.get(key) ?? 0) + amount);
    for (const row of data?.billing_month_client_totals ?? []) {
      const client = String(row.client_id);
      const month = String(row.month);
      const billed = Number(row.billed_rub ?? 0);
      add(`${client}|${String(row.project_id ?? "")}|${month}`, billed);
      add(`${client}||${month}`, billed);
    }
    return map;
  }, [data?.billing_month_client_totals]);

  const moneyTotals = useMemo(() => {
    const totals = { incoming: 0, review: 0, reviewCount: 0, received: 0, overdue: 0, estimated: 0 };
    const estimateUsed = new Set<string>();
    for (const row of data?.receivables ?? []) {
      const monthKey = String(row.due_on ?? row.invoice_on ?? row.period_start ?? "").slice(0, 7);
      if (!monthKey.startsWith(periodPrefix)) continue;
      const status = String(row.status);
      if (status === "skipped" || status === "written_off") continue;
      const planned = Number(row.planned_amount_rub ?? 0);
      const paid = Number(row.paid_amount_rub ?? 0);
      totals.received += paid;
      const rest = Math.max(planned - paid, 0);
      if (isTruthyFlag(row.needs_review)) {
        totals.review += rest;
        totals.reviewCount += 1;
      }
      else totals.incoming += rest;
      if (isTruthyFlag(row.is_overdue)) totals.overdue += rest;
      if (planned === 0 && paid === 0) {
        const meta = agreementMeta.get(String(row.agreement_id));
        const estKey = `${String(row.client_id)}|${meta?.project ?? ""}|${monthKey}`;
        if (meta && (meta.model === "time_material" || meta.model === "cap") && !estimateUsed.has(estKey)) {
          const billed = billingByClientMonth.get(estKey) ?? 0;
          const estimated = meta.cap > 0 ? Math.min(billed, meta.cap) : billed;
          if (estimated > 0) {
            totals.estimated += estimated;
            estimateUsed.add(estKey);
          }
        }
      }
    }
    return totals;
  }, [data?.receivables, periodPrefix, agreementMeta, billingByClientMonth]);

  const calendarGroups = useMemo(() => {
    const groups = new Map<string, { rows: BusinessRow[]; planned: number; paid: number; overdue: number; estimated: number }>();
    const estimateUsed = new Set<string>();
    for (const row of filteredReceivables) {
      const monthKey = String(row.due_on ?? row.invoice_on ?? row.period_start ?? "").slice(0, 7) || "—";
      if (!monthKey.startsWith(periodPrefix)) continue;
      const status = String(row.status);
      if ((status === "skipped" || status === "written_off") && !receivableStatuses.includes(status)) continue;
      const planned = Number(row.planned_amount_rub ?? 0);
      const paid = Number(row.paid_amount_rub ?? 0);
      const meta = agreementMeta.get(String(row.agreement_id));
      let estimated = 0;
      if (planned === 0 && paid === 0 && meta && (meta.model === "time_material" || meta.model === "cap")) {
        const estKey = `${String(row.client_id)}|${meta.project}|${monthKey}`;
        if (!estimateUsed.has(estKey)) {
          const billed = billingByClientMonth.get(estKey) ?? 0;
          estimated = meta.cap > 0 ? Math.min(billed, meta.cap) : billed;
          if (estimated > 0) estimateUsed.add(estKey);
        }
      }
      if (hideZeroReceivables && planned === 0 && paid === 0 && estimated === 0) continue;
      const entry = groups.get(monthKey) ?? { rows: [], planned: 0, paid: 0, overdue: 0, estimated: 0 };
      const reviewDetails: string[] = [];
      if (planned <= 0) reviewDetails.push(t(($) => $.money.missing_amount));
      if (!row.invoice_on) reviewDetails.push(t(($) => $.money.missing_invoice_date));
      if (!row.due_on) reviewDetails.push(t(($) => $.money.missing_due_date));
      if (!(data?.payers ?? []).some((payer) => String(payer.client_id) === String(row.client_id))) reviewDetails.push(t(($) => $.money.missing_payer));
      entry.rows.push({
        ...row,
        agreement_name: meta?.name || String(row.project_title ?? row.period_key ?? ""),
        planned_amount_rub: planned > 0 ? row.planned_amount_rub : (estimated > 0 ? estimated : row.planned_amount_rub),
        by_tasks: estimated > 0,
        review_details: isTruthyFlag(row.needs_review) ? reviewDetails.join(", ") : "",
      });
      if (status !== "skipped" && status !== "written_off") {
        entry.planned += planned > 0 ? planned : estimated;
        entry.paid += paid;
        entry.estimated += estimated;
        if (isTruthyFlag(row.is_overdue)) entry.overdue += 1;
      }
      groups.set(monthKey, entry);
    }
    return [...groups.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([monthKey, entry]) => ({
        monthKey,
        rows: entry.rows.sort((a, b) => String(a.due_on ?? "9999").localeCompare(String(b.due_on ?? "9999"))),
        planned: entry.planned,
        paid: entry.paid,
        overdue: entry.overdue,
        estimated: entry.estimated,
      }));
  }, [filteredReceivables, hideZeroReceivables, agreementMeta, billingByClientMonth, periodPrefix, receivableStatuses, data?.payers, t]);

  const periodTransactions = useMemo(() => (data?.transactions ?? []).filter((row) => String(row.booked_on ?? "").startsWith(periodPrefix)), [data?.transactions, periodPrefix]);

  const unresolvedCounterparties = useMemo(() => groupUnresolvedBankCounterparties(periodTransactions), [periodTransactions]);

  // Receipts booked before the ledger existed have nothing to reconcile
  // against, and the register goes back to 2018. The first receivable is where
  // "unmatched" starts meaning "needs work".
  const reconcileFrom = useMemo(() => {
    let earliest = "";
    for (const row of data?.receivables ?? []) {
      const start = String(row.period_start ?? "").slice(0, 10);
      if (start && (earliest === "" || start < earliest)) earliest = start;
    }
    return earliest;
  }, [data?.receivables]);

  const isUnreconciled = (row: BusinessRow): boolean => (
    String(row.direction) === "inbound"
    && !["transfer", "vitmax_transit"].includes(String(row.classification))
    && !isTruthyFlag(row.is_matched)
  );

  const filteredTransactions = useMemo(() => {
    const needle = txSearch.trim().toLowerCase();
    return periodTransactions.filter((row) =>
      (txClasses.length === 0 || txClasses.includes(String(row.classification)))
      && (txDirections.length === 0 || txDirections.includes(String(row.direction)))
      && (!txUnmatchedOnly || (isUnreconciled(row) && (reconcileFrom === "" || String(row.booked_on ?? "") >= reconcileFrom)))
      && (!needle || `${String(row.counterparty_name ?? "")} ${String(row.purpose ?? "")} ${String(row.counterparty_inn ?? "")}`.toLowerCase().includes(needle))
    ).map((row) => ({ ...row, match_state: isTruthyFlag(row.is_matched) ? "matched" : "unmatched" }) as BusinessRow);
  }, [periodTransactions, txClasses, txDirections, txUnmatchedOnly, txSearch, reconcileFrom]);

  // Never a silent cut: say how many receipts the ledger date dropped.
  const legacyUnmatchedCount = useMemo(() => {
    if (!txUnmatchedOnly || reconcileFrom === "") return 0;
    return periodTransactions.filter((row) => isUnreconciled(row) && String(row.booked_on ?? "") < reconcileFrom).length;
  }, [periodTransactions, txUnmatchedOnly, reconcileFrom]);

  const taskBillingGroups = useMemo(() => {
    const groups = new Map<string, { month: string; client_name: string; task_amount_rub: number; issues: Map<string, string> }>();
    for (const row of data?.billing_tasks ?? []) {
      const taskMonth = String(row.month ?? "");
      if (!taskMonth.startsWith(periodPrefix)) continue;
      const clientName = String(row.client_name ?? "—");
      const key = `${taskMonth}|${String(row.client_id ?? "")}`;
      const entry = groups.get(key) ?? { month: taskMonth, client_name: clientName, task_amount_rub: 0, issues: new Map<string, string>() };
      entry.task_amount_rub += Number(row.price_rub ?? 0);
      entry.issues.set(String(row.issue_id), `${String(row.project_title ?? "")} · ${String(row.issue_title ?? row.issue_id)}`);
      groups.set(key, entry);
    }
    return [...groups.values()].map((entry) => ({
      month: entry.month,
      client_name: entry.client_name,
      task_count: entry.issues.size,
      task_amount_rub: entry.task_amount_rub,
      tasks_preview: [...entry.issues.values()].join("; "),
    })).sort((a, b) => b.month.localeCompare(a.month) || a.client_name.localeCompare(b.client_name));
  }, [data?.billing_tasks, periodPrefix]);

  const transactionTotals = useMemo(() => filteredTransactions.reduce<{ inbound: number; outbound: number }>((acc, row) => {
    const amount = Number(row.amount_rub ?? 0);
    return String(row.direction) === "inbound"
      ? { ...acc, inbound: acc.inbound + amount }
      : { ...acc, outbound: acc.outbound + amount };
  }, { inbound: 0, outbound: 0 }), [filteredTransactions]);

  const workerMonths = useMemo(() => {
    const map = new Map<string, { worker_name: string; month: string; accrued_rub: number; funded_rub: number; paid_rub: number }>();
    for (const row of data?.accruals ?? []) {
      const period = monthOf(row.created_at);
      const name = String(row.worker_name ?? row.worker_id);
      const key = `${name}|${period}`;
      const entry = map.get(key) ?? { worker_name: name, month: period, accrued_rub: 0, funded_rub: 0, paid_rub: 0 };
      entry.accrued_rub += Number(row.original_amount_rub ?? 0) + Number(row.adjustment_rub ?? 0);
      entry.funded_rub += Number(row.funded_rub ?? 0) + Number(row.reserve_funded_rub ?? 0);
      entry.paid_rub += Number(row.paid_rub ?? 0);
      map.set(key, entry);
    }
    return [...map.values()].sort((a, b) => b.month.localeCompare(a.month) || a.worker_name.localeCompare(b.worker_name));
  }, [data?.accruals]);

  const enrichedPayoutItems = useMemo(() => {
    const batches = new Map<string, BusinessRow>();
    for (const row of data?.payout_batches ?? []) batches.set(String(row.id), row);
    return (data?.payout_items ?? []).map((row) => {
      const batch = batches.get(String(row.payout_batch_id));
      return { ...row, period_key: batch?.period_key ?? "—", batch_status: batch?.status ?? "—" };
    });
  }, [data?.payout_items, data?.payout_batches]);

  const bankInns = useMemo(() => {
    const set = new Set<string>();
    for (const row of data?.transactions ?? []) {
      const inn = String(row.counterparty_inn ?? "");
      if (inn) set.add(inn);
    }
    return set;
  }, [data?.transactions]);

  const clientRows = useMemo(() => filteredClients.map((client) => {
    const id = String(client.id);
    const projects = (data?.projects ?? []).filter((row) => String(row.client_id) === id);
    const payers = (data?.payers ?? []).filter((row) => String(row.client_id) === id);
    return {
      ...client,
      projects_list: projects.map((row) => String(row.project_title ?? "")).filter(Boolean).join(", "),
      payers_list: payers.map((row) => `${String(row.name)}${row.inn ? ` · ${String(row.inn)}` : ""}`).join("; "),
      elba: payers.some((row) => Boolean(row.elba_contractor_id)),
      bank_linked: payers.some((row) => Boolean(row.inn) && bankInns.has(String(row.inn))),
    };
  }), [filteredClients, data?.projects, data?.payers, bankInns]);

  const filteredAccruals = useMemo(() => (data?.accruals ?? []).filter((row) =>
    (accrualWorkers.length === 0 || accrualWorkers.includes(String(row.worker_id)))
    && (accrualStatuses.length === 0 || accrualStatuses.includes(String(row.status)))
  ), [data?.accruals, accrualWorkers, accrualStatuses]);

  const filteredEconomics = useMemo(() => (data?.task_economics ?? []).filter((row) =>
    (econStatuses.length === 0 || econStatuses.includes(String(row.status)))
    && (econClients.length === 0 || econClients.includes(String(row.client_id)))
  ), [data?.task_economics, econStatuses, econClients]);

  const filteredBatches = useMemo(() => (data?.payout_batches ?? []).filter((row) =>
    payoutStatuses.length === 0 || payoutStatuses.includes(String(row.status))
  ), [data?.payout_batches, payoutStatuses]);

  const enrichedParticipants = useMemo(() => {
    const economics = new Map<string, BusinessRow>();
    for (const row of data?.task_economics ?? []) economics.set(String(row.id), row);
    return (data?.task_participants ?? []).map((row) => {
      const econ = economics.get(String(row.task_economics_id));
      return { ...row, issue_title: econ?.issue_title ?? "—", status: econ?.status ?? "—" };
    });
  }, [data?.task_participants, data?.task_economics]);

  if (!enabled) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.unavailable)}</div>;
  if (accounts.isLoading || (businessID && (dashboard.isLoading || snapshot.isLoading))) return <div className="flex min-h-[50vh] items-center justify-center p-8 text-muted-foreground">{t(($) => $.loading)}</div>;
  if (accounts.error || dashboard.error || snapshot.error) return <div className="p-8 text-destructive">{String(accounts.error ?? dashboard.error ?? snapshot.error)}</div>;
  if (!businessID || !data || !dashboard.data) return <div className="p-8 text-muted-foreground">{t(($) => $.empty)}</div>;

  const metrics = dashboard.data;
  const clientOptions = (data.clients ?? []).map((row) => ({ value: String(row.id), label: String(row.canonical_name) }));
  const projectOptions = (data.projects ?? []).map((row) => ({ value: String(row.project_id), label: String(row.project_title ?? row.project_id) }));
  const matchableReceivables = (data.receivables ?? []).filter(receivableAcceptsPayment);
  const clientChannels = new Map((data.clients ?? []).map((row) => [String(row.id), String(row.primary_payment_channel ?? "bank")]));
  const paymentChannelFor = (row: BusinessRow): string =>
    agreementMeta.get(String(row.agreement_id))?.channel || clientChannels.get(String(row.client_id)) || "bank";
  // Money that goes through the business account is reconciled from the bank
  // statement, so a manual entry there would only duplicate it. The action
  // belongs to the agreements that are paid past the account — card or cash.
  const offersManualPayment = (row: BusinessRow): boolean =>
    receivableAcceptsPayment(row) && paymentChannelFor(row) !== "bank";
  const paymentReceivable = (data.receivables ?? []).find((row) => String(row.id) === paymentReceivableID) ?? null;
  const serviceOptions = SERVICE_TYPES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) }));
  const econWorkerRow = (data.workers ?? []).find((row) => String(row.id) === econWorker) ?? (data.workers ?? [])[0];
  const effWorkerID = econWorker || String(econWorkerRow?.id ?? "");
  const effRole = econRole || String(econWorkerRow?.default_role ?? "") || "executor";
  const effPercent = econPercent !== "" ? econPercent : (econWorkerRow?.default_percent ? String(Number(econWorkerRow.default_percent)) : "25");
  const tabs: { key: Tab; enabled: boolean }[] = [
    { key: "overview", enabled: true }, { key: "calendar", enabled: calendarEnabled }, { key: "clients", enabled: clientsEnabled },
    { key: "billing", enabled: clientsEnabled }, { key: "bank", enabled: bankEnabled },
    { key: "costs", enabled: true },
    { key: "economics", enabled: economicsEnabled }, { key: "team", enabled: economicsEnabled },
  ];
  const openClientRow = (data.clients ?? []).find((row) => String(row.id) === openClientID) ?? null;
  const single = (setter: (next: string[]) => void, current: string[]) => (value: string) => {
    setter(current.includes(value) ? [] : [value]);
  };

  const headerFilter = ((): { sections: FilterSection[]; toggles?: FilterToggle[]; onClear: () => void } | null => {
    switch (tab) {
      case "overview":
        return {
          onClear: () => { setScopeClient([]); setScopeProject([]); setScopeService([]); },
          sections: [
            { key: "client", label: t(($) => $.filters.client), options: clientOptions, selected: scopeClient, onToggle: single(setScopeClient, scopeClient) },
            { key: "project", label: t(($) => $.filters.project), options: projectOptions, selected: scopeProject, onToggle: single(setScopeProject, scopeProject) },
            { key: "service", label: t(($) => $.filters.service), options: serviceOptions, selected: scopeService, onToggle: single(setScopeService, scopeService) },
          ],
        };
      case "clients":
        return {
          onClear: () => { setClientStatuses([]); setCounterpartyClasses([]); },
          sections: [
            { key: "status", label: t(($) => $.filters.status), options: CLIENT_STATUSES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: clientStatuses, onToggle: (value) => setClientStatuses(toggleValue(clientStatuses, value)) },
            { key: "class", label: t(($) => $.filters.classification), options: COUNTERPARTY_CLASSES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: counterpartyClasses, onToggle: (value) => setCounterpartyClasses(toggleValue(counterpartyClasses, value)) },
          ],
        };
      case "calendar":
        return {
          onClear: () => { setReceivableStatuses([]); setReceivableClients([]); setReceivableReviewOnly(false); setReceivableOverdueOnly(false); },
          sections: [
            { key: "status", label: t(($) => $.filters.status), options: RECEIVABLE_STATUSES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: receivableStatuses, onToggle: (value) => setReceivableStatuses(toggleValue(receivableStatuses, value)) },
            { key: "client", label: t(($) => $.filters.client), options: clientOptions, selected: receivableClients, onToggle: (value) => setReceivableClients(toggleValue(receivableClients, value)) },
          ],
          toggles: [
            { key: "review", label: t(($) => $.filters.only_review), checked: receivableReviewOnly, onToggle: () => setReceivableReviewOnly(!receivableReviewOnly) },
            { key: "overdue", label: t(($) => $.filters.only_overdue), checked: receivableOverdueOnly, onToggle: () => setReceivableOverdueOnly(!receivableOverdueOnly) },
            { key: "zero", label: t(($) => $.filters.hide_empty), checked: hideZeroReceivables, defaultChecked: true, onToggle: () => setHideZeroReceivables(!hideZeroReceivables) },
          ],
        };
      case "bank":
        return {
          onClear: () => { setTxClasses([]); setTxDirections([]); setCounterpartyClasses([]); setTxUnmatchedOnly(false); },
          sections: [
            { key: "class", label: t(($) => $.filters.classification), options: CLASSIFICATIONS.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: txClasses, onToggle: (value) => setTxClasses(toggleValue(txClasses, value)) },
            { key: "direction", label: t(($) => $.filters.direction), options: [{ value: "inbound", label: t(($) => $.values.inbound) }, { value: "outbound", label: t(($) => $.values.outbound) }], selected: txDirections, onToggle: (value) => setTxDirections(toggleValue(txDirections, value)) },
            { key: "cp", label: t(($) => $.sections.counterparties), options: COUNTERPARTY_CLASSES.map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: counterpartyClasses, onToggle: (value) => setCounterpartyClasses(toggleValue(counterpartyClasses, value)) },
          ],
          toggles: [
            { key: "unmatched", label: t(($) => $.filters.only_unmatched), checked: txUnmatchedOnly, onToggle: () => setTxUnmatchedOnly(!txUnmatchedOnly) },
          ],
        };
      case "economics":
        return {
          onClear: () => { setEconStatuses([]); setEconClients([]); },
          sections: [
            { key: "status", label: t(($) => $.filters.status), options: ["draft", "accepted", "superseded"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: econStatuses, onToggle: (value) => setEconStatuses(toggleValue(econStatuses, value)) },
            { key: "client", label: t(($) => $.filters.client), options: clientOptions, selected: econClients, onToggle: (value) => setEconClients(toggleValue(econClients, value)) },
          ],
        };
      case "team":
        return {
          onClear: () => { setAccrualWorkers([]); setAccrualStatuses([]); setPayoutStatuses([]); },
          sections: [
            { key: "worker", label: t(($) => $.fields.worker), options: (data.workers ?? []).map((row) => ({ value: String(row.id), label: String(row.name) })), selected: accrualWorkers, onToggle: (value) => setAccrualWorkers(toggleValue(accrualWorkers, value)) },
            { key: "status", label: t(($) => $.sections.accruals), options: ["accrued", "partially_payable", "payable", "in_payout", "paid", "adjusted"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: accrualStatuses, onToggle: (value) => setAccrualStatuses(toggleValue(accrualStatuses, value)) },
            { key: "payout", label: t(($) => $.sections.payouts), options: ["draft", "approved", "submitted", "paid", "failed", "reconciled"].map((value) => ({ value, label: tt(`values.${value}`, { defaultValue: value }) })), selected: payoutStatuses, onToggle: (value) => setPayoutStatuses(toggleValue(payoutStatuses, value)) },
          ],
        };
      default:
        return null;
    }
  })();

  const seriesPoints = series.data?.points ?? [];
  const chartConfig = {
    plan: { label: t(($) => $.metrics.expected), color: "var(--chart-1)" },
    fact: { label: t(($) => $.metrics.client_income), color: "var(--chart-2)" },
  } satisfies ChartConfig;
  const chartData = seriesPoints.map((point) => ({
    label: periodMode === "month" ? point.month.slice(8) : point.month.slice(5),
    plan: Number(point.expected_rub),
    fact: Number(point.bank_income_rub),
  }));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-2 px-5 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Building2 className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {accounts.data && accounts.data.length > 1 && (
            <NativeSelect size="sm" aria-label={t(($) => $.title)} value={businessID} onChange={(event) => setSelectedBusiness(event.target.value)}>
              {accounts.data.map((account) => <NativeSelectOption key={account.id} value={account.id}>{account.name}</NativeSelectOption>)}
            </NativeSelect>
          )}
          <div className="flex items-center gap-0.5 rounded-lg border p-0.5">
            <Button type="button" variant={periodMode === "month" ? "secondary" : "ghost"} size="sm" className="h-7 px-2 text-xs" onClick={() => setPeriodMode("month")}>{t(($) => $.month)}</Button>
            <Button type="button" variant={periodMode === "year" ? "secondary" : "ghost"} size="sm" className="h-7 px-2 text-xs" onClick={() => setPeriodMode("year")}>{t(($) => $.year)}</Button>
          </div>
          <div className="flex items-center gap-0.5">
            <Button type="button" variant="ghost" size="sm" className="h-8 w-7 px-0 text-muted-foreground" onClick={() => { if (periodMode === "year") { setMonth(`${Number(periodYear) - 1}${month.slice(4)}`); } else { const d = new Date(`${month}-15T00:00:00`); d.setMonth(d.getMonth() - 1); setMonth(d.toISOString().slice(0, 7)); } }}><ChevronLeft className="size-4" /></Button>
            {periodMode === "month"
              ? <Input className="h-8 w-auto max-w-36 text-xs" type="month" value={month} onChange={(event) => setMonth(event.target.value)} />
              : <span className="px-1 text-xs font-medium tabular-nums">{periodYear}</span>}
            <Button type="button" variant="ghost" size="sm" className="h-8 w-7 px-0 text-muted-foreground" onClick={() => { if (periodMode === "year") { setMonth(`${Number(periodYear) + 1}${month.slice(4)}`); } else { const d = new Date(`${month}-15T00:00:00`); d.setMonth(d.getMonth() + 1); setMonth(d.toISOString().slice(0, 7)); } }}><ChevronRight className="size-4" /></Button>
          </div>
          {tab === "bank" && bankEnabled && (
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                data-testid="bank-counterparty-search"
                value={txSearch}
                onChange={(event) => setTxSearch(event.target.value)}
                placeholder={t(($) => $.filters.search)}
                className="h-8 w-56 pl-8 text-sm"
              />
            </div>
          )}
          {headerFilter && (
            <FilterMenu
              label={t(($) => $.filters.filter)}
              clearLabel={t(($) => $.actions.clear_filters)}
              sections={headerFilter.sections}
              toggles={headerFilter.toggles}
              onClear={headerFilter.onClear}
            />
          )}
          {tab === "bank" && bankEnabled && (
            <>
              <input
                ref={bankImportInputRef}
                type="file"
                accept=".csv,.txt,.xml,.ofx,.pdf,text/csv,application/xml,text/xml"
                className="hidden"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (!file || !businessID) return;
                  void execute("import", () => api.importBusinessBankFile(businessID, file));
                }}
              />
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-8"
                disabled={busy !== "" || !businessID}
                onClick={() => bankImportInputRef.current?.click()}
              >
                {t(($) => $.actions.import_statement)}
              </Button>
            </>
          )}
          {tab === "clients" && clientsEnabled && (
            <Button type="button" size="sm" className="h-8 gap-1.5" onClick={() => setCreateClientOpen(true)}>
              <Plus className="size-3.5" />
              <span className="hidden sm:inline">{t(($) => $.actions.add_client)}</span>
            </Button>
          )}
          {tab === "costs" && (
            <Button type="button" size="sm" className="h-8 gap-1.5" onClick={() => setCreateCostOpen(true)}>
              <Plus className="size-3.5" />
              <span className="hidden sm:inline">{t(($) => $.actions.add_cost)}</span>
            </Button>
          )}
        </div>
      </PageHeader>

      <Tabs
        value={tab}
        onValueChange={(value) => setTab(value as Tab)}
        orientation={isMobile ? "horizontal" : "vertical"}
        className="flex min-h-0 flex-1 flex-col gap-0 md:flex-row"
      >
        <div className="shrink-0 overflow-x-auto border-b p-2 md:w-44 md:overflow-y-auto md:border-b-0 md:border-r md:p-3">
          <TabsList variant="line" className="flex w-max min-w-full flex-row items-center gap-1 p-0 md:w-full md:flex-col md:items-stretch">
            {tabs.filter((item) => item.enabled).map(({ key }) => {
              const Icon = TAB_ICONS[key];
              return (
                <TabsTrigger key={key} value={key} className={NAV_TAB_TRIGGER_CLASS}>
                  <Icon className="h-4 w-4" />
                  {t(($) => $.tabs[key])}
                </TabsTrigger>
              );
            })}
          </TabsList>
        </div>

        <div data-testid="business-scroll-container" className="min-h-0 flex-1 overflow-y-auto @container">
          <div className="mx-auto w-full max-w-6xl space-y-4 p-4 sm:p-5">

      {(error || message) && <div className={cn("rounded-lg border p-3 text-sm", error ? "border-destructive/40 bg-destructive/5 text-destructive" : "border-emerald-500/40 bg-emerald-500/5 text-emerald-700")}>{error || message}</div>}

      {tab === "overview" && <div className="space-y-5">
        <p className="text-xs text-muted-foreground">{t(($) => $.subtitle)}</p>

        <Section title={t(($) => $.metric_groups.calendar)}>
          <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-3 @5xl:grid-cols-5">
            <Metric label={t(($) => $.metrics.expected)} value={rub(metrics.expected_rub)} />
            <Metric label={t(($) => $.metrics.invoiced)} value={rub(metrics.invoiced_rub)} />
            <Metric label={t(($) => $.metrics.receivable_paid)} value={rub(metrics.receivable_paid_rub)} />
            <Metric label={t(($) => $.metrics.overdue)} value={rub(metrics.overdue_rub)} hint={`${t(($) => $.filters.rows)}: ${metrics.overdue_count ?? 0}`} warning={Number(metrics.overdue_rub) > 0} onClick={() => { setReceivableStatuses([]); setReceivableClients([]); setReceivableOverdueOnly(true); setReceivableReviewOnly(false); setTab("calendar"); }} />
            <Metric label={t(($) => $.metrics.not_invoiced)} value={rub(metrics.not_invoiced_rub)} warning={Number(metrics.not_invoiced_rub) > 0} />
          </div>
        </Section>

        {/* Ceilings sit next to the payment calendar rather than inside the
            money tab: they are the one number worth catching mid-period, and
            the overview is where the owner looks first. */}
        <Section title={t(($) => $.sections.caps)}>
          <p className="text-[11px] text-muted-foreground">{t(($) => $.cap.hint)}</p>
          {cappedAgreements.length === 0 ? (
            <div className="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">{t(($) => $.cap.empty)}</div>
          ) : (
            <div className="grid gap-2 @3xl:grid-cols-2 @5xl:grid-cols-3">
              {cappedAgreements.map((agreement) => (
                <div key={String(agreement.id)} className="space-y-1.5 rounded-lg border p-2.5">
                  <button
                    type="button"
                    className="text-left text-xs font-medium hover:underline"
                    onClick={() => setOpenClientID(String(agreement.client_id ?? ""))}
                  >
                    {String(agreement.client_name ?? "")}
                    <span className="text-muted-foreground"> · {String(agreement.name ?? "")}</span>
                  </button>
                  <BusinessCapMeter agreement={agreement} charges={data?.billing_tasks ?? []} locale={locale} />
                </div>
              ))}
            </div>
          )}
        </Section>

        <Section title={t(($) => $.metric_groups.bank)}>
          <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4 @5xl:grid-cols-5">
            <Metric label={t(($) => $.metrics.client_income)} value={rub(metrics.bank_client_income_rub)} hint={Number(metrics.transit_body_rub ?? 0) > 0 ? t(($) => $.metrics.client_income_hint) : undefined} />
            {Number(metrics.transit_body_rub ?? 0) > 0 && (
              <Metric
                label={t(($) => $.metrics.transit_body)}
                value={rub(metrics.transit_body_rub)}
                hint={`${t(($) => $.metrics.transit_commission)}: ${rub(metrics.transit_commission_rub)} · ${t(($) => $.metrics.transit_net)}: ${rub(metrics.transit_net_rub)}`}
              />
            )}
            <Metric label={t(($) => $.metrics.unmatched)} value={rub(metrics.unknown_inbound_rub)} hint={`${t(($) => $.filters.rows)}: ${metrics.unmatched_count ?? 0}`} warning={(metrics.unmatched_count ?? 0) > 0} onClick={() => { setTxClasses([]); setTxDirections(["inbound"]); setTxUnmatchedOnly(true); setTab("bank"); }} />
            <Metric label={t(($) => $.values.vitmax)} value={rub(metrics.vitmax_transit_rub)} />
            <Metric label={t(($) => $.values.transfers)} value={rub(metrics.transfer_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.economics)}>
          <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4">
            <Metric label={t(($) => $.metrics.task_value)} value={rub(metrics.task_value_rub)} />
            <Metric label={t(($) => $.metrics.participant_accrued)} value={rub(metrics.participant_accrued_rub)} />
            <Metric label={t(($) => $.metrics.company_pool)} value={rub(metrics.company_target_pool_rub)} hint={`${t(($) => $.metrics.company_costs)}: ${rub(metrics.company_costs_rub)}`} />
            <Metric label={t(($) => $.metrics.owner_margin)} value={rub(metrics.owner_target_margin_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.team)}>
          <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4">
            <Metric label={t(($) => $.metrics.payable)} value={rub(metrics.payable_rub)} />
            <Metric label={t(($) => $.metrics.paid_workers)} value={rub(metrics.paid_to_workers_rub)} />
            <Metric label={t(($) => $.metrics.reserve)} value={rub(metrics.reserve_balance_rub)} warning={Number(metrics.reserve_deficit_rub) > 0} />
            <Metric label={t(($) => $.metrics.reserve_obligation)} value={rub(metrics.reserve_obligation_rub)} />
          </div>
        </Section>

        <Section title={t(($) => $.metric_groups.summary)}>
          <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-3">
            <Metric label={t(($) => $.metrics.owner_net)} value={rub(metrics.owner_net_income_rub)} />
            <Metric label={t(($) => $.metrics.target)} value={`${metrics.owner_target_progress_pct}%`} hint={rub(metrics.owner_income_target_rub)} />
            <Metric label={t(($) => $.metrics.company_costs)} value={rub(metrics.company_costs_rub)} />
          </div>
        </Section>

        <div className="grid gap-2 @3xl:grid-cols-2">
          <div className="rounded-lg border p-3 text-xs text-muted-foreground"><AlertTriangle className="mr-1.5 inline size-3.5 text-warning"/>{t(($) => $.vitmax_note)}</div>
          <div className="rounded-lg border p-3 text-xs text-muted-foreground">{t(($) => $.no_penalties_note)}</div>
        </div>
      </div>}

      {tab === "clients" && clientsEnabled && <div className="space-y-6">
        <Section title={t(($) => $.sections.clients)} actions={<ResultCount shown={filteredClients.length} total={(data.clients ?? []).length} />}>
          <div className="space-y-1.5">
            <div className="text-[11px] text-muted-foreground">{t(($) => $.card.hint)}</div>
            <RowTable tt={tt} locale={locale} rows={clientRows as unknown as BusinessRow[]} columns={[{ key: "canonical_name" }, { key: "status", kind: "enum" }, { key: "primary_payment_channel", kind: "enum" }, { key: "projects_list" }, { key: "payers_list" }, { key: "elba", kind: "bool" }, { key: "bank_linked", kind: "bool" }, { key: "notes" }]} empty={t(($) => $.empty)} onRowClick={(row) => setOpenClientID(String(row.id))} />
          </div>
        </Section>
      </div>}

      {tab === "billing" && clientsEnabled && (
        <BusinessBillingTab businessID={businessID} data={data} onChanged={() => snapshot.refetch()} />
      )}

      {tab === "costs" && (
        <BusinessCostsTab
          businessID={businessID}
          month={month}
          periodMode={periodMode}
          costs={data.recurring_costs ?? []}
          createOpen={createCostOpen}
          onCreateOpenChange={setCreateCostOpen}
          onChanged={() => Promise.all([snapshot.refetch(), dashboard.refetch()])}
        />
      )}

      {tab === "calendar" && calendarEnabled && <div className="space-y-6">
        <div className="grid grid-cols-2 gap-2 @3xl:grid-cols-4">
          <Metric label={t(($) => $.money.incoming)} value={rub(moneyTotals.incoming)} hint={moneyTotals.estimated > 0 ? `+ ≈ ${rub(moneyTotals.estimated)} ${t(($) => $.money.estimated)}` : undefined} />
          <Metric label={t(($) => $.money.on_review)} value={rub(moneyTotals.review)} hint={`${t(($) => $.filters.rows)}: ${moneyTotals.reviewCount} · ${t(($) => $.money.on_review_hint)}`} warning={moneyTotals.reviewCount > 0} onClick={() => { setReceivableStatuses([]); setReceivableClients([]); setReceivableReviewOnly(true); setReceivableOverdueOnly(false); }} />
          <Metric label={t(($) => $.money.received)} value={rub(moneyTotals.received)} />
          <Metric label={t(($) => $.metrics.overdue)} value={rub(moneyTotals.overdue)} warning={moneyTotals.overdue > 0} onClick={() => { setReceivableStatuses([]); setReceivableClients([]); setReceivableOverdueOnly(true); setReceivableReviewOnly(false); }} />
        </div>
        {(() => {
          const base = moneyTotals.incoming + moneyTotals.review + moneyTotals.estimated + moneyTotals.received;
          if (base <= 0) return null;
          return (
            <div className="rounded-lg border p-3 text-xs text-muted-foreground">
              {t(($) => $.money.split)} ({rub(base)}): {t(($) => $.money.team)} ≈ <span className="font-medium text-foreground">{rub(base * 0.35)}</span>
              {" · "}{t(($) => $.money.company)} ≈ <span className="font-medium text-foreground">{rub(base * 0.15)}</span>
              {" · "}{t(($) => $.money.owner)} ≈ <span className="font-medium text-foreground">{rub(base * 0.5)}</span>
            </div>
          );
        })()}
        <Section title={t(($)=>$.sections.receivables)}>
          {calendarGroups.length === 0 ? (
            <div className="rounded-lg border border-dashed p-6 text-center text-xs text-muted-foreground">{t(($) => $.empty)}</div>
          ) : (
            <div className="space-y-4">
              {calendarGroups.map((group) => (
                <div key={group.monthKey} className="space-y-1.5">
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <h3 className="text-sm font-medium">{monthLabel(group.monthKey, locale)}</h3>
                    <span className="text-xs tabular-nums text-muted-foreground">
                      {tt("columns.expected_rub", { defaultValue: "План" })}: <span className="font-medium text-foreground">{rub(group.planned)}</span>
                      {group.estimated > 0 && <> ({"≈"} {rub(group.estimated)} {t(($) => $.money.estimated)})</>}
                      {" · "}{t(($) => $.metrics.receivable_paid)}: <span className="font-medium text-foreground">{rub(group.paid)}</span>
                      {group.overdue > 0 && <>{" · "}<span className="font-medium text-destructive">{t(($) => $.metrics.overdue)}: {group.overdue}</span></>}
                    </span>
                  </div>
                  <RowTable
                    tt={tt}
                    locale={locale}
                    rows={group.rows}
                    columns={[{ key: "client_name" }, { key: "agreement_name" }, { key: "planned_amount_rub", kind: "money" }, { key: "by_tasks", kind: "bool" }, { key: "paid_amount_rub", kind: "money" }, { key: "due_on", kind: "date" }, { key: "status", kind: "receivable_status" }]}
                    empty={t(($) => $.empty)}
                    extra={{
                      header: "",
                      render: (row) => offersManualPayment(row) ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          className="h-6 whitespace-nowrap px-2 text-[11px]"
                          data-testid="record-payment-trigger"
                          disabled={busy !== ""}
                          onClick={() => setPaymentReceivableID(String(row.id))}
                        >
                          {t(($) => $.actions.record_payment)}
                        </Button>
                      ) : null,
                    }}
                  />
                </div>
              ))}
            </div>
          )}
        </Section>
        <Section title={t(($) => $.sections.task_calculation)}>
          <p className="text-[11px] text-muted-foreground">{t(($) => $.money.task_calculation_hint)}</p>
          <RowTable
            tt={tt}
            locale={locale}
            rows={taskBillingGroups as unknown as BusinessRow[]}
            columns={[{ key: "month" }, { key: "client_name" }, { key: "task_count" }, { key: "task_amount_rub", kind: "money" }, { key: "tasks_preview" }]}
            empty={t(($) => $.money.no_task_calculation)}
          />
        </Section>
        {/* Plan against fact is a money question, so it sits with the money
            rather than in the overview's wall of metrics. */}
        <Section title={periodMode === "month" ? t(($) => $.sections.month_dynamics) : t(($) => $.sections.year_dynamics)}>
          <div className="space-y-2">
            {chartData.length > 0 && (
              <div className="rounded-lg border p-3">
                <ChartContainer config={chartConfig} className="aspect-[5/2] w-full @3xl:aspect-[3/1]">
                  <BarChart data={chartData} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
                    <CartesianGrid vertical={false} />
                    <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} />
                    <YAxis tickLine={false} axisLine={false} tickMargin={4} width={64} tickFormatter={(value: number) => new Intl.NumberFormat("ru-RU", { notation: "compact", maximumFractionDigits: 1 }).format(value)} />
                    <ChartTooltip content={<ChartTooltipContent formatter={(value, name) => `${rub(Number(value))} · ${String(chartConfig[name as keyof typeof chartConfig]?.label ?? name)}`} />} />
                    <Bar dataKey="plan" fill="var(--color-plan)" radius={[3, 3, 0, 0]} />
                    <Bar dataKey="fact" fill="var(--color-fact)" radius={[3, 3, 0, 0]} />
                  </BarChart>
                </ChartContainer>
              </div>
            )}
            <RowTable tt={tt} locale={locale} rows={seriesPoints as unknown as BusinessRow[]} columns={[{ key: "month" }, { key: "expected_rub", kind: "money" }, { key: "bank_income_rub", kind: "money" }]} empty={t(($) => $.empty)} />
          </div>
        </Section>

      </div>}

      {tab === "bank" && bankEnabled && <div className="space-y-6">
        <Toolbar>
          <div className="flex min-w-0 items-center gap-2">
            <ResultCount shown={filteredTransactions.length} total={periodTransactions.length} />
            {legacyUnmatchedCount > 0 && (
              <span className="whitespace-nowrap text-xs text-muted-foreground" data-testid="legacy-unmatched">
                {t(($) => $.bank.before_ledger)}: {legacyUnmatchedCount}
              </span>
            )}
            <span data-testid="bank-summary" className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground lg:inline">
              {t(($) => $.values.inbound)}: <span className="font-medium text-foreground">{rub(transactionTotals.inbound)}</span>
              {" · "}{t(($) => $.values.outbound)}: <span className="font-medium text-foreground">{rub(transactionTotals.outbound)}</span>
              {" · "}{t(($) => $.bank.source)}: <span className="font-medium text-foreground">{t(($) => $.bank.modulbank_import)}</span>
              {(data.bank_imports ?? [])[0] && <>{" · "}{t(($) => $.bank.last_import)}: <span className="font-medium text-foreground">{new Date(String((data.bank_imports ?? [])[0]?.created_at)).toLocaleString(locale, { dateStyle: "short", timeStyle: "short" })}</span></>}
            </span>
          </div>
        </Toolbar>
        {unresolvedCounterparties.length > 0 && <Section title={t(($) => $.sections.unresolved_counterparties)}>
          <div data-testid="unresolved-counterparties-section" className="space-y-1.5">
            <p className="text-[11px] text-muted-foreground">{t(($) => $.bank.unresolved_hint)}</p>
            <RowTable
              tt={tt}
              locale={locale}
              rows={unresolvedCounterparties}
              columns={[{ key: "counterparty_name" }, { key: "counterparty_inn" }, { key: "operation_count" }, { key: "inbound_rub", kind: "money" }, { key: "outbound_rub", kind: "money" }]}
              empty={t(($) => $.bank.all_resolved)}
              extra={{ header: t(($) => $.actions.resolve), render: (row) => (
                <form className="flex min-w-80 items-center gap-1" onSubmit={(event) => {
                  const fd = formData(event);
                  const resolution = parseCounterpartyResolutionTarget(String(fd.get("target") ?? ""));
                  if (!resolution) return;
                  const transactionID = counterpartyResolutionTransactionID(row, resolution);
                  if (!transactionID) return;
                  void execute(`counterparty-${String(row.transaction_id)}`, () => api.businessAction(businessID, "bank/counterparties/resolve", {
                    transaction_id: transactionID,
                    ...resolution,
                    reason: "manual bank counterparty resolution",
                  }));
                }}>
                  <NativeSelect size="sm" required name="target" aria-label={t(($) => $.bank.resolve_as)}>
                    <NativeSelectOption value="">{t(($) => $.bank.resolve_as)}</NativeSelectOption>
                    {String(row.inbound_transaction_id ?? "") !== "" && (data.clients ?? []).map((client) => <NativeSelectOption key={`client-${String(client.id)}`} value={`client:${String(client.id)}`}>{t(($) => $.bank.client_payer_prefix)} · {String(client.canonical_name)}</NativeSelectOption>)}
                    {String(row.outbound_transaction_id ?? "") !== "" && (data.workers ?? []).map((worker) => <NativeSelectOption key={`worker-${String(worker.id)}`} value={`worker:${String(worker.id)}`}>{t(($) => $.bank.worker_payee_prefix)} · {String(worker.name)}</NativeSelectOption>)}
                    <NativeSelectOption value="class:vendor">{tt("values.vendor", { defaultValue: "vendor" })}</NativeSelectOption>
                    <NativeSelectOption value="class:transit">{tt("values.transit", { defaultValue: "transit" })}</NativeSelectOption>
                    <NativeSelectOption value="class:ignored">{tt("values.ignored", { defaultValue: "ignored" })}</NativeSelectOption>
                  </NativeSelect>
                  <Button type="submit" size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy !== ""}>{t(($) => $.actions.save_rule)}</Button>
                </form>
              ) }}
            />
          </div>
        </Section>}
        <Section title={t(($)=>$.sections.transactions)}>
          <RowTable tt={tt} locale={locale} rows={filteredTransactions} columns={[{ key: "booked_on", kind: "date" }, { key: "direction", kind: "enum" }, { key: "amount_rub", kind: "money" }, { key: "counterparty_name" }, { key: "counterparty_inn" }, { key: "classification", kind: "enum" }, { key: "match_state", kind: "enum" }, { key: "purpose" }]} empty={t(($)=>$.empty)}
            extra={{ header: t(($) => $.actions.resolve), render: (row) => (
              <div className="flex min-w-80 flex-col gap-1">
                <form className="flex items-center gap-1" onSubmit={(event) => { const fd = formData(event); void execute(`classify-${String(row.id)}`, () => api.businessAction(businessID, `bank/transactions/${String(row.id)}/classify`, { classification: fd.get("cls"), confidence: "confirmed", reason: "manual reclassification" })); }}>
                  <NativeSelect size="sm" name="cls" defaultValue={String(row.classification ?? "unknown")}>{CLASSIFICATIONS.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
                  <Button type="submit" size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                </form>
                {String(row.direction) === "inbound" && !isTruthyFlag(row.is_matched) && (
                  <form className="flex items-center gap-1" onSubmit={(event) => { const fd = formData(event); const targetID = String(fd.get("receivable") ?? ""); const target = matchableReceivables.find((item) => String(item.id) === targetID); const remaining = Math.max(Number(target?.planned_amount_rub ?? 0) - Number(target?.paid_amount_rub ?? 0), 0); const amount = Math.min(Number(row.amount_rub ?? 0), remaining); if (targetID && amount > 0) void execute(`match-${String(row.id)}`, () => api.businessAction(businessID, `bank/transactions/${String(row.id)}/matches`, { target_type: "receivable", target_id: targetID, amount_rub: String(amount), status: "confirmed", idempotency_key: crypto.randomUUID(), notes: "manual bank match" })); }}>
                    <NativeSelect size="sm" required name="receivable" aria-label={t(($) => $.sections.receivables)}>
                      <NativeSelectOption value="">{t(($) => $.actions.choose_receivable)}</NativeSelectOption>
                      {matchableReceivables.map((item) => <NativeSelectOption key={String(item.id)} value={String(item.id)}>{String(item.client_name)} · {String(item.period_key)} · {rub(Math.max(Number(item.planned_amount_rub ?? 0) - Number(item.paid_amount_rub ?? 0), 0))}</NativeSelectOption>)}
                    </NativeSelect>
                    <Button type="submit" size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy !== ""}>{t(($) => $.actions.match_receipt)}</Button>
                  </form>
                )}
              </div>
            ) }} />
        </Section>
        <Section title={t(($) => $.sections.counterparty_rules)}>
          <div className="space-y-1.5">
            <p className="text-[11px] text-muted-foreground">{t(($) => $.bank.rules_hint)}</p>
            <RowTable tt={tt} locale={locale} rows={filteredCounterparties} columns={[{ key: "name" }, { key: "inn" }, { key: "classification", kind: "enum" }, { key: "confidence", kind: "enum" }, { key: "reason" }]} empty={t(($) => $.empty)} />
          </div>
        </Section>
      </div>}

      {tab === "team" && economicsEnabled && <div className="space-y-6">
        <Section title={t(($)=>$.sections.workers)} actions={<form className="flex items-center gap-1.5" onSubmit={(event)=>{const fd=formData(event);void execute("worker",()=>api.businessAction(businessID,"workers",{name:fd.get("name"),engagement_format:"self_employed"}));event.currentTarget.reset();}}><Input required name="name" className="h-8 w-48 text-sm" placeholder={t(($)=>$.fields.name)}/><Button type="submit" size="sm" className="h-8" disabled={busy!==""}>{t(($)=>$.actions.add_worker)}</Button></form>}>
          <div className="space-y-1.5">
            <div className="text-[11px] text-muted-foreground">{t(($) => $.team.rate_hint)}</div>
            <div className="rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="h-8 text-xs font-medium text-muted-foreground">{tt("columns.name", { defaultValue: "name" })}</TableHead>
                    <TableHead className="h-8 text-xs font-medium text-muted-foreground">{tt("columns.status", { defaultValue: "status" })}</TableHead>
                    <TableHead className="h-8 text-xs font-medium text-muted-foreground">{tt("columns.engagement_format", { defaultValue: "format" })}</TableHead>
                    <TableHead className="h-8 text-xs font-medium text-muted-foreground">{t(($) => $.team.default_rate)}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(data.workers ?? []).map((row) => {
                    const id = String(row.id);
                    return (
                      <TableRow key={id}>
                        <TableCell className="py-1.5 text-xs font-medium">{text(row, "name")}</TableCell>
                        <TableCell className="py-1.5 text-xs"><StatusPill value={String(row.status)} label={tt(`values.${String(row.status)}`, { defaultValue: String(row.status) })} /></TableCell>
                        <TableCell className="py-1.5 text-xs"><StatusPill value={String(row.engagement_format)} label={tt(`values.${String(row.engagement_format)}`, { defaultValue: String(row.engagement_format) })} /></TableCell>
                        <TableCell className="py-1 text-xs">
                          <form className="flex flex-wrap items-center gap-1.5" onSubmit={(event) => { const fd = formData(event); void execute(`rate-${id}`, () => api.businessAction(businessID, `workers/${id}`, { default_role: fd.get("role"), default_percent: fd.get("percent") }, "PATCH")); }}>
                            <NativeSelect size="sm" name="role" defaultValue={String(row.default_role ?? "")}>
                              <NativeSelectOption value="">{t(($) => $.team.no_rate)}</NativeSelectOption>
                              {WORKER_ROLES.map((value) => <NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}
                            </NativeSelect>
                            <Input name="percent" inputMode="decimal" className="h-7 w-16 text-xs" defaultValue={row.default_percent ? String(Number(row.default_percent)) : ""} placeholder="%" />
                            <Button type="submit" size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy !== ""}>{t(($) => $.actions.save)}</Button>
                          </form>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                  {(data.workers ?? []).length === 0 && <TableRow><TableCell colSpan={4} className="p-6 text-center text-xs text-muted-foreground">{t(($) => $.empty)}</TableCell></TableRow>}
                </TableBody>
              </Table>
            </div>
          </div>
        </Section>
        <Section title={t(($) => $.sections.worker_months)}>
          <RowTable tt={tt} locale={locale} rows={workerMonths as unknown as BusinessRow[]} columns={[{ key: "worker_name" }, { key: "month" }, { key: "accrued_rub", kind: "money" }, { key: "funded_rub", kind: "money" }, { key: "paid_rub", kind: "money" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($) => $.sections.payout_items)}>
          <RowTable tt={tt} locale={locale} rows={enrichedPayoutItems} columns={[{ key: "worker_name" }, { key: "period_key" }, { key: "amount_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "batch_status", kind: "enum" }, { key: "created_at", kind: "datetime" }]} empty={t(($) => $.empty)} />
        </Section>
        <Section title={t(($)=>$.sections.accruals)}><RowTable tt={tt} locale={locale} rows={filteredAccruals} columns={[{ key: "worker_name" }, { key: "role", kind: "enum" }, { key: "original_amount_rub", kind: "money" }, { key: "adjustment_rub", kind: "money" }, { key: "funded_rub", kind: "money" }, { key: "reserve_funded_rub", kind: "money" }, { key: "paid_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "reserve_due_on", kind: "date" }]} empty={t(($)=>$.empty)}/></Section>
        {payoutsEnabled && <Section title={t(($)=>$.sections.payouts)} actions={<form className="flex items-center gap-1.5" onSubmit={(event)=>{const fd=formData(event);void execute("payout",()=>api.businessAction(businessID,"payouts",{period_key:fd.get("period"),idempotency_key:`payout:${String(fd.get("period"))}`}));}}><Input required name="period" type="month" defaultValue={month} className="h-8 w-auto text-sm"/><Button type="submit" size="sm" variant="outline" className="h-8" disabled={busy!==""}>{t(($)=>$.actions.build_payout)}</Button></form>}>
          <div className="space-y-2">
            <div className="text-[11px] text-muted-foreground">{t(($)=>$.bank_draft_note)}</div>
            <RowTable tt={tt} locale={locale} rows={filteredBatches} columns={[{ key: "period_key" }, { key: "status", kind: "enum" }, { key: "total_rub", kind: "money" }, { key: "worker_count" }, { key: "approved_at", kind: "datetime" }, { key: "paid_at", kind: "datetime" }]} empty={t(($)=>$.empty)}/>
            <div className="flex flex-wrap gap-1.5">{filteredBatches.map((row)=>{const status=text(row,"status"),id=text(row,"id");return <div key={id} className="flex gap-1">{status==="draft"&&<Button type="button" variant="outline" size="sm" className="h-7 text-xs" disabled={busy!==""} onClick={()=>void execute("approve",()=>api.businessAction(businessID,`payouts/${id}/approve`))}>{t(($)=>$.actions.approve)} · {text(row,"period_key")}</Button>}{status==="approved"&&bankDraftsEnabled&&<Button type="button" variant="outline" size="sm" className="h-7 text-xs" disabled={busy!==""} onClick={()=>void execute("submit",()=>api.businessAction(businessID,`payouts/${id}/submit-draft`))}>{t(($)=>$.actions.submit_draft)} · {text(row,"period_key")}</Button>}</div>})}</div>
          </div>
        </Section>}
        <Section title={t(($)=>$.sections.reserve)} actions={<form className="flex items-center gap-1.5" onSubmit={(event)=>{const fd=formData(event);void execute("reserve",()=>api.businessAction(businessID,"reserve/entries",{entry_type:"contribution",amount_rub:fd.get("amount"),reason:fd.get("reason"),idempotency_key:crypto.randomUUID()}));event.currentTarget.reset();}}><Input required name="amount" className="h-7 w-28 text-xs" placeholder={t(($)=>$.fields.amount)}/><Input required name="reason" className="h-7 w-56 text-xs" placeholder={t(($)=>$.fields.reason)}/><Button type="submit" size="sm" variant="outline" className="h-7 text-xs" disabled={busy!==""}>{t(($)=>$.actions.add_reserve)}</Button></form>}><RowTable tt={tt} locale={locale} rows={data.reserve_ledger ?? []} columns={[{ key: "occurred_at", kind: "datetime" }, { key: "entry_type", kind: "enum" }, { key: "amount_rub", kind: "money" }, { key: "reason" }]} empty={t(($)=>$.empty)}/></Section>
      </div>}

      {tab === "economics" && economicsEnabled && <div className="space-y-6">
        <p className="text-xs text-muted-foreground">{t(($) => $.economics_hint)}</p>
        <Section title={t(($) => $.sections.billing_candidates)} actions={
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-xs text-muted-foreground">{t(($) => $.fields.worker)}</span>
            <NativeSelect size="sm" value={effWorkerID} onChange={(event) => { setEconWorker(event.target.value); setEconRole(""); setEconPercent(""); }}>{(data.workers ?? []).map((row)=><NativeSelectOption key={text(row,"id")} value={text(row,"id")}>{text(row,"name")}</NativeSelectOption>)}</NativeSelect>
            <NativeSelect size="sm" value={effRole} onChange={(event) => setEconRole(event.target.value)}>{WORKER_ROLES.map((value)=><NativeSelectOption key={value} value={value}>{tt(`values.${value}`, { defaultValue: value })}</NativeSelectOption>)}</NativeSelect>
            <Input value={effPercent} onChange={(event) => setEconPercent(event.target.value)} inputMode="decimal" className="h-7 w-14 text-xs" placeholder="%" />
          </div>
        }>
          <RowTable tt={tt} locale={locale} rows={data.billing_candidates ?? []} columns={[{ key: "issue_title" }, { key: "project_title" }, { key: "client_name" }, { key: "price_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "created_at", kind: "date" }]} empty={t(($) => $.empty)}
            extra={{ header: t(($) => $.actions.estimate), render: (row) => (
              <Button type="button" size="sm" variant="outline" className="h-6 px-2 text-[11px]" disabled={busy !== "" || !effWorkerID} onClick={() => void execute(`estimate-${String(row.id)}`, () => api.businessAction(businessID, "task-economics", { workspace_id: row.workspace_id, project_id: row.project_id, issue_id: row.issue_id, client_id: row.client_id || null, service_type: row.service_type || "development", service_value_rub: String(row.price_rub ?? 0), source: "charge_snapshot", billing_disposition: "normal", idempotency_key: crypto.randomUUID(), participants: [{ worker_id: effWorkerID, role: effRole, pool: effRole === "pm" ? "pm" : "execution", percent: effPercent }] }))}>{t(($) => $.actions.estimate)}</Button>
            ) }} />
        </Section>
        <Section title={t(($)=>$.sections.tasks)}><div className="space-y-2"><RowTable tt={tt} locale={locale} rows={filteredEconomics} columns={[{ key: "issue_title" }, { key: "project_title" }, { key: "client_name" }, { key: "service_type", kind: "enum" }, { key: "service_value_rub", kind: "money" }, { key: "status", kind: "enum" }, { key: "pm_eligible", kind: "bool" }, { key: "accepted_at", kind: "datetime" }]} empty={t(($)=>$.empty)}/><div className="flex flex-wrap gap-1.5">{acceptEnabled&&accrualsEnabled&&(data.task_economics ?? []).filter((row)=>text(row,"status")==="draft").map((row)=><Button type="button" variant="outline" size="sm" className="h-7 text-xs" key={text(row,"id")} disabled={busy!==""} onClick={()=>void execute("accept",()=>api.businessAction(businessID,`task-economics/${text(row,"id")}/accept`,{reason:"owner acceptance"}))}>{t(($)=>$.actions.accept)} · {text(row,"issue_title") !== "—" ? text(row,"issue_title") : text(row,"issue_id")}</Button>)}</div></div></Section>
        <Section title={t(($) => $.sections.participations)}>
          <RowTable tt={tt} locale={locale} rows={enrichedParticipants} columns={[{ key: "worker_name" }, { key: "issue_title" }, { key: "role", kind: "enum" }, { key: "pool", kind: "enum" }, { key: "percent", kind: "percent" }, { key: "amount_rub", kind: "money" }, { key: "status", kind: "enum" }]} empty={t(($) => $.empty)} />
        </Section>
      </div>}

          </div>
        </div>
      </Tabs>

      <BusinessClientCard
        businessID={businessID}
        client={openClientRow}
        data={data}
        onClose={() => setOpenClientID("")}
        onChanged={async () => { await Promise.all([snapshot.refetch(), dashboard.refetch()]); }}
      />

      <BusinessReceivablePaymentSheet
        businessID={businessID}
        receivable={paymentReceivable}
        defaultChannel={paymentReceivable ? paymentChannelFor(paymentReceivable) : "personal_card"}
        onClose={() => setPaymentReceivableID("")}
        onRecorded={async () => { await Promise.all([snapshot.refetch(), dashboard.refetch()]); }}
      />

      <BusinessClientCreateSheet
        businessID={businessID}
        availableProjects={data.available_projects ?? []}
        open={createClientOpen}
        onOpenChange={setCreateClientOpen}
        onCreated={async (clientID) => {
          await Promise.all([snapshot.refetch(), dashboard.refetch()]);
          if (clientID) setOpenClientID(clientID);
        }}
      />
    </div>
  );
}
