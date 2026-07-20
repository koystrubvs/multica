"use client";

import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { api } from "@multica/core/api";
import type { BusinessRow } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
import { useFxResolver } from "../common/use-fx-rates";
import { useT } from "../i18n";

type PeriodMode = "month" | "year";

interface CostFormState {
  id: string;
  name: string;
  amount: string;
  currency: "USD" | "RUB";
  category: string;
  chargeDay: string;
  startsOn: string;
  endsOn: string;
  notes: string;
}

function emptyForm(month: string): CostFormState {
  return {
    id: "",
    name: "",
    amount: "",
    currency: "USD",
    category: "ai",
    chargeDay: "15",
    startsOn: `${month}-01`,
    endsOn: "",
    notes: "",
  };
}

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
  return (!startsOn || startsOn <= month) && (!endsOn || endsOn >= month);
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
  return rows.reduce((total, row) => total + months.reduce((monthTotal, month) => {
    if (!activeInMonth(row, month)) return monthTotal;
    const amount = Number(row.amount ?? 0);
    const rate = String(row.currency) === "USD"
      ? resolveFx(chargeDate(month, Number(row.charge_day ?? 1)))
      : 1;
    return monthTotal + amount * rate;
  }, 0), 0);
}

export function BusinessCostsTab({ businessID, month, periodMode, costs, onChanged }: {
  businessID: string;
  month: string;
  periodMode: PeriodMode;
  costs: BusinessRow[];
  onChanged: () => Promise<unknown>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as (key: string, options?: { defaultValue?: string }) => string;
  const [form, setForm] = useState(() => emptyForm(month));
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const months = useMemo(() => monthList(month, periodMode), [month, periodMode]);
  const from = `${months[0]}-01`;
  const lastMonth = months.at(-1) ?? month;
  const to = chargeDate(lastMonth, 31);
  const { resolve, loaded } = useFxResolver(from, to, businessID);
  const periodTotal = calculateRecurringCostTotal(costs, months, resolve);

  const resetForm = () => setForm(emptyForm(month));
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    const payload = {
      name: form.name,
      category: form.category,
      amount: form.amount,
      currency: form.currency,
      charge_day: Number(form.chargeDay),
      starts_on: form.startsOn,
      ends_on: form.endsOn,
      notes: form.notes,
    };
    try {
      if (form.id) {
        await api.businessAction(businessID, `recurring-costs/${form.id}`, payload, "PATCH");
      } else {
        await api.businessAction(businessID, "recurring-costs", payload);
      }
      await onChanged();
      resetForm();
      setMessage(tt("costs.saved"));
    } catch {
      setMessage(tt("costs.save_error"));
    } finally {
      setBusy(false);
    }
  };

  const edit = (row: BusinessRow) => setForm({
    id: String(row.id),
    name: String(row.name ?? ""),
    amount: String(row.amount ?? ""),
    currency: String(row.currency) === "RUB" ? "RUB" : "USD",
    category: String(row.category ?? "service"),
    chargeDay: String(row.charge_day ?? 15),
    startsOn: String(row.starts_on ?? "").slice(0, 10),
    endsOn: String(row.ends_on ?? "").slice(0, 10),
    notes: String(row.notes ?? ""),
  });

  const setStatus = async (row: BusinessRow, status: string) => {
    setBusy(true);
    setMessage("");
    try {
      await api.businessAction(businessID, `recurring-costs/${String(row.id)}`, { status }, "PATCH");
      await onChanged();
    } catch {
      setMessage(tt("costs.save_error"));
    } finally {
      setBusy(false);
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

      <form className="space-y-3 rounded-lg border p-4" onSubmit={submit}>
        <div className="text-sm font-medium">{form.id ? tt("costs.edit_cost") : tt("costs.new_cost")}</div>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <Input required value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder={tt("costs.name")} />
          <div className="flex gap-2">
            <Input required min="0.01" step="0.01" type="number" value={form.amount} onChange={(event) => setForm({ ...form, amount: event.target.value })} placeholder={tt("costs.amount")} />
            <NativeSelect value={form.currency} onChange={(event) => setForm({ ...form, currency: event.target.value as "USD" | "RUB" })}>
              <NativeSelectOption value="USD">USD</NativeSelectOption>
              <NativeSelectOption value="RUB">RUB</NativeSelectOption>
            </NativeSelect>
          </div>
          <NativeSelect value={form.category} onChange={(event) => setForm({ ...form, category: event.target.value })}>
            {["ai", "service", "infrastructure", "contractor", "tax", "bank", "other"].map((value) => (
              <NativeSelectOption key={value} value={value}>{tt(`costs.categories.${value}`)}</NativeSelectOption>
            ))}
          </NativeSelect>
          <Input required min="1" max="31" type="number" value={form.chargeDay} onChange={(event) => setForm({ ...form, chargeDay: event.target.value })} placeholder={tt("costs.charge_day")} />
          <Input required type="date" value={form.startsOn} onChange={(event) => setForm({ ...form, startsOn: event.target.value })} aria-label={tt("costs.starts_on")} />
          <Input type="date" value={form.endsOn} onChange={(event) => setForm({ ...form, endsOn: event.target.value })} aria-label={tt("costs.ends_on")} />
          <Input className="sm:col-span-2" value={form.notes} onChange={(event) => setForm({ ...form, notes: event.target.value })} placeholder={tt("costs.notes")} />
        </div>
        <div className="flex items-center gap-2">
          <Button disabled={busy} size="sm">{tt("costs.save")}</Button>
          {form.id && <Button disabled={busy} type="button" variant="ghost" size="sm" onClick={resetForm}>{tt("costs.cancel")}</Button>}
          {message && <span className="text-xs text-muted-foreground">{message}</span>}
        </div>
      </form>

      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{tt("costs.name")}</TableHead>
            <TableHead>{tt("costs.schedule")}</TableHead>
            <TableHead>{tt("costs.amount")}</TableHead>
            <TableHead>{tt("costs.status")}</TableHead>
            <TableHead className="text-right">{tt("costs.actions")}</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {costs.length === 0 && <TableRow><TableCell colSpan={5} className="py-8 text-center text-muted-foreground">{tt("costs.empty")}</TableCell></TableRow>}
            {costs.map((row) => {
              const status = String(row.status);
              return <TableRow key={String(row.id)}>
                <TableCell><div className="font-medium">{String(row.name)}</div>{row.notes ? <div className="text-xs text-muted-foreground">{String(row.notes)}</div> : null}</TableCell>
                <TableCell>{tt("costs.day_of_month", { defaultValue: "{{day}}" }).replace("{{day}}", String(row.charge_day))}</TableCell>
                <TableCell className="tabular-nums">{Number(row.amount).toLocaleString("ru-RU")} {String(row.currency)}</TableCell>
                <TableCell>{tt(`costs.statuses.${status}`)}</TableCell>
                <TableCell className="space-x-1 text-right">
                  <Button disabled={busy} type="button" variant="ghost" size="sm" onClick={() => edit(row)}>{tt("costs.edit")}</Button>
                  {status !== "archived" && <Button disabled={busy} type="button" variant="ghost" size="sm" onClick={() => void setStatus(row, status === "active" ? "paused" : "active")}>{status === "active" ? tt("costs.pause") : tt("costs.resume")}</Button>}
                  {status !== "archived" && <Button disabled={busy} type="button" variant="ghost" size="sm" onClick={() => void setStatus(row, "archived")}>{tt("costs.archive")}</Button>}
                </TableCell>
              </TableRow>;
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
