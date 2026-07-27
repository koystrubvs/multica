"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@multica/core/api";
import type { BusinessRow } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import { MoreHorizontal } from "lucide-react";
import { useFxResolver } from "../common/use-fx-rates";
import { useT } from "../i18n";
import { BusinessCostSheet } from "./business-cost-sheet";

type PeriodMode = "month" | "year";

function monthList(month: string, periodMode: PeriodMode): string[] {
  if (periodMode === "month") return [month];
  return Array.from({ length: 12 }, (_, index) => `${month.slice(0, 4)}-${String(index + 1).padStart(2, "0")}`);
}

function chargeDate(month: string, chargeDay: number): string {
  const [year = 1970, monthNumber = 1] = month.split("-").map(Number);
  const lastDay = new Date(Date.UTC(year, monthNumber, 0)).getUTCDate();
  return `${month}-${String(Math.min(Math.max(chargeDay, 1), lastDay)).padStart(2, "0")}`;
}

function activeInMonth(row: BusinessRow, month: string): boolean {
  if (String(row.status) !== "active") return false;
  const startsOn = String(row.starts_on ?? "").slice(0, 7);
  const endsOn = String(row.ends_on ?? "").slice(0, 7);
  if ((startsOn && startsOn > month) || (endsOn && endsOn < month)) return false;
  return String(row.frequency ?? "monthly") !== "yearly" || startsOn.slice(5, 7) === month.slice(5, 7);
}

function scheduleLabel(
  row: BusinessRow,
  tt: (key: string, options?: { defaultValue?: string }) => string,
): string {
  const day = String(row.charge_day);
  if (String(row.frequency ?? "monthly") !== "yearly") {
    return tt("costs.day_of_month", { defaultValue: "{{day}}" }).replace("{{day}}", day);
  }
  const month = String(row.starts_on ?? "").slice(5, 7);
  return tt("costs.day_of_year", { defaultValue: "{{day}}.{{month}}" })
    .replace("{{day}}", day)
    .replace("{{month}}", month);
}

function rub(value: number): string {
  return new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 0,
  }).format(Number.isFinite(value) ? value : 0);
}

export function calculateRecurringCostTotal(
  rows: BusinessRow[],
  months: string[],
  resolveFx: (date: string) => number,
): number {
  const total = rows.reduce((sum, row) => sum + months.reduce((monthTotal, month) => {
    if (!activeInMonth(row, month)) return monthTotal;
    const amount = Number(row.amount ?? 0);
    const rate = String(row.currency) === "USD"
      ? resolveFx(chargeDate(month, Number(row.charge_day ?? 1)))
      : 1;
    return monthTotal + amount * rate;
  }, 0), 0);
  return Math.round(total / 100) * 100;
}

function CostRow({
  row,
  busy,
  highlighted,
  onEdit,
  onSetStatus,
  tt,
}: {
  row: BusinessRow;
  busy: boolean;
  highlighted: boolean;
  onEdit: () => void;
  onSetStatus: (status: string) => void;
  tt: (key: string, options?: { defaultValue?: string }) => string;
}) {
  const status = String(row.status);
  const triggerRef = useRef<HTMLButtonElement>(null);
  return (
    <TableRow
      className={cn("group/row cursor-pointer", highlighted && "bg-muted/50")}
      onClick={() => triggerRef.current?.click()}
    >
      <TableCell>
        <div className="font-medium">{String(row.name)}</div>
        {row.notes ? <div className="text-xs text-muted-foreground">{String(row.notes)}</div> : null}
      </TableCell>
      <TableCell>{scheduleLabel(row, tt)}</TableCell>
      <TableCell className="tabular-nums">
        {Number(row.amount).toLocaleString("ru-RU")} {String(row.currency)}
      </TableCell>
      <TableCell>{tt(`costs.statuses.${status}`)}</TableCell>
      <TableCell className="text-right" onClick={(event) => event.stopPropagation()}>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                ref={triggerRef}
                type="button"
                disabled={busy}
                aria-label={tt("costs.row_menu")}
                className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground data-popup-open:bg-accent data-popup-open:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
              >
                <MoreHorizontal className="size-4" />
              </button>
            }
          />
          <DropdownMenuContent align="end" className="w-48">
            <div className="px-1.5 py-1 text-xs font-medium text-muted-foreground">
              {String(row.name)}
            </div>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onEdit}>{tt("costs.edit")}</DropdownMenuItem>
            {status !== "archived" && (
              <DropdownMenuItem
                disabled={busy}
                onClick={() => onSetStatus(status === "active" ? "paused" : "active")}
              >
                {status === "active" ? tt("costs.pause") : tt("costs.resume")}
              </DropdownMenuItem>
            )}
            {status !== "archived" && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  variant="destructive"
                  disabled={busy}
                  onClick={() => onSetStatus("archived")}
                >
                  {tt("costs.archive")}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

export function BusinessCostsTab({
  businessID,
  month,
  periodMode,
  costs,
  createOpen,
  onCreateOpenChange,
  onChanged,
}: {
  businessID: string;
  month: string;
  periodMode: PeriodMode;
  costs: BusinessRow[];
  createOpen: boolean;
  onCreateOpenChange: (open: boolean) => void;
  onChanged: () => Promise<unknown>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as (key: string, options?: { defaultValue?: string }) => string;
  const [editCost, setEditCost] = useState<BusinessRow | null>(null);
  const [busyKey, setBusyKey] = useState("");
  const [message, setMessage] = useState("");
  const months = useMemo(() => monthList(month, periodMode), [month, periodMode]);
  const from = `${months[0]}-01`;
  const lastMonth = months.at(-1) ?? month;
  const to = chargeDate(lastMonth, 31);
  const { resolve, loaded } = useFxResolver(from, to, businessID);
  const periodTotal = calculateRecurringCostTotal(costs, months, resolve);
  const sheetOpen = createOpen || editCost !== null;
  const sheetCost = createOpen ? null : editCost;

  useEffect(() => {
    if (createOpen) setEditCost(null);
  }, [createOpen]);

  const setStatus = async (row: BusinessRow, status: string) => {
    const id = String(row.id);
    setBusyKey(id);
    setMessage("");
    try {
      await api.businessAction(businessID, `recurring-costs/${id}`, { status }, "PATCH");
      await onChanged();
    } catch {
      setMessage(tt("costs.save_error"));
    } finally {
      setBusyKey("");
    }
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border bg-card p-4">
        <div className="text-xs text-muted-foreground">{tt("costs.period_total")}</div>
        <div className="mt-1 text-2xl font-semibold tabular-nums">{rub(periodTotal)}</div>
        <div className="mt-1 text-xs text-muted-foreground">
          {loaded ? tt("costs.fx_note") : tt("costs.fx_loading")}
        </div>
      </div>

      {message && (
        <div className="text-xs text-muted-foreground" role="status">{message}</div>
      )}

      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{tt("costs.name")}</TableHead>
              <TableHead>{tt("costs.schedule")}</TableHead>
              <TableHead>{tt("costs.amount")}</TableHead>
              <TableHead>{tt("costs.status")}</TableHead>
              <TableHead className="w-10">
                <span className="sr-only">{tt("costs.actions")}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {costs.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                  {tt("costs.empty")}
                </TableCell>
              </TableRow>
            )}
            {costs.map((row) => {
              const id = String(row.id);
              return (
                <CostRow
                  key={id}
                  row={row}
                  busy={busyKey === id}
                  highlighted={editCost !== null && String(editCost.id) === id}
                  onEdit={() => {
                    onCreateOpenChange(false);
                    setEditCost(row);
                  }}
                  onSetStatus={(next) => void setStatus(row, next)}
                  tt={tt}
                />
              );
            })}
          </TableBody>
        </Table>
      </div>

      <BusinessCostSheet
        businessID={businessID}
        month={month}
        open={sheetOpen}
        cost={sheetCost}
        onOpenChange={(open) => {
          if (!open) {
            onCreateOpenChange(false);
            setEditCost(null);
            return;
          }
          if (!editCost) onCreateOpenChange(true);
        }}
        onSaved={async () => {
          setMessage(tt("costs.saved"));
          await onChanged();
        }}
      />
    </div>
  );
}
