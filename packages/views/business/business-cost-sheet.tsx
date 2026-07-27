"use client";

import { useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { ApiError, api } from "@multica/core/api";
import type { BusinessRow } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useT } from "../i18n";

const FIELD_LABEL = "text-[11px] font-medium uppercase tracking-wide text-muted-foreground";
const FIELD_HINT = "text-[11px] text-muted-foreground";
const CATEGORIES = ["ai", "service", "infrastructure", "contractor", "tax", "bank", "other"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

interface CostFormState {
  id: string;
  name: string;
  amount: string;
  currency: "USD" | "RUB";
  category: string;
  frequency: "monthly" | "yearly";
  chargeDay: string;
  /** YYYY-MM-01 */
  startsOn: string;
  /** YYYY-MM-01 or "" */
  endsOn: string;
  notes: string;
}

function toMonthStart(value: string, fallback: string): string {
  const raw = String(value || "").trim();
  if (/^\d{4}-\d{2}$/.test(raw)) return `${raw}-01`;
  if (/^\d{4}-\d{2}-\d{2}/.test(raw)) return `${raw.slice(0, 7)}-01`;
  return fallback;
}

function monthValue(isoDay: string): string {
  return isoDay ? isoDay.slice(0, 7) : "";
}

function emptyForm(month: string): CostFormState {
  return {
    id: "",
    name: "",
    amount: "",
    currency: "USD",
    category: "ai",
    frequency: "monthly",
    chargeDay: "20",
    startsOn: `${month}-01`,
    endsOn: "",
    notes: "",
  };
}

function formFromRow(row: BusinessRow, month: string): CostFormState {
  const fallback = `${month}-01`;
  return {
    id: String(row.id ?? ""),
    name: String(row.name ?? ""),
    amount: String(row.amount ?? ""),
    currency: String(row.currency) === "RUB" ? "RUB" : "USD",
    category: String(row.category ?? "service"),
    frequency: String(row.frequency) === "yearly" ? "yearly" : "monthly",
    chargeDay: String(row.charge_day ?? 20),
    startsOn: toMonthStart(String(row.starts_on ?? ""), fallback),
    endsOn: row.ends_on ? toMonthStart(String(row.ends_on), "") : "",
    notes: String(row.notes ?? ""),
  };
}

function validateForm(form: CostFormState, tt: TT): string | null {
  if (!form.name.trim()) return tt("costs.validation.name");
  const amount = Number(form.amount);
  if (!Number.isFinite(amount) || amount <= 0) return tt("costs.validation.amount");
  const day = Number(form.chargeDay);
  if (!Number.isInteger(day) || day < 1 || day > 31) return tt("costs.validation.charge_day");
  if (!/^\d{4}-\d{2}-01$/.test(form.startsOn)) return tt("costs.validation.starts_on");
  if (form.endsOn) {
    if (!/^\d{4}-\d{2}-01$/.test(form.endsOn)) return tt("costs.validation.ends_on");
    if (form.endsOn < form.startsOn) return tt("costs.validation.ends_before_starts");
  }
  return null;
}

function mapApiError(message: string, tt: TT): string {
  const lower = message.toLowerCase();
  if (lower.includes("month must use the first day") || lower.includes("invalid end month")) {
    return tt("costs.validation.starts_on");
  }
  if (lower.includes("amount")) return tt("costs.validation.amount");
  if (lower.includes("charge day")) return tt("costs.validation.charge_day");
  if (lower.includes("name")) return tt("costs.validation.name");
  if (lower.includes("recurring cost fields are invalid")) return tt("costs.validation.generic");
  return message || tt("costs.save_error");
}

function schedulePreview(form: CostFormState, tt: TT): string {
  const day = String(Math.min(Math.max(Number(form.chargeDay) || 1, 1), 31));
  if (form.frequency !== "yearly") {
    return tt("costs.day_of_month", { defaultValue: "{{day}}" }).replace("{{day}}", day);
  }
  const month = form.startsOn.slice(5, 7) || "01";
  return tt("costs.day_of_year", { defaultValue: "{{day}}.{{month}}" })
    .replace("{{day}}", day)
    .replace("{{month}}", month);
}

export function BusinessCostSheet({
  businessID,
  month,
  open,
  cost,
  onOpenChange,
  onSaved,
}: {
  businessID: string;
  month: string;
  open: boolean;
  cost: BusinessRow | null;
  onOpenChange: (open: boolean) => void;
  onSaved: () => Promise<unknown>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as TT;
  const [form, setForm] = useState(() => emptyForm(month));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const editing = Boolean(form.id);
  const preview = useMemo(() => schedulePreview(form, tt), [form, tt]);

  useEffect(() => {
    if (!open) return;
    setError("");
    setForm(cost ? formFromRow(cost, month) : emptyForm(month));
  }, [open, cost, month]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const localError = validateForm(form, tt);
    if (localError) {
      setError(localError);
      return;
    }
    setBusy(true);
    setError("");
    const payload = {
      name: form.name.trim(),
      category: form.category,
      amount: form.amount,
      currency: form.currency,
      frequency: form.frequency,
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
      onOpenChange(false);
      await onSaved();
    } catch (cause) {
      const message = cause instanceof ApiError || cause instanceof Error ? cause.message : "";
      setError(mapApiError(message, tt));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) setError("");
        onOpenChange(next);
      }}
    >
      <SheetContent side="right" className="w-full data-[side=right]:sm:max-w-md">
        <SheetHeader>
          <SheetTitle className="text-base">
            {editing ? tt("costs.edit_cost") : t(($) => $.actions.add_cost)}
          </SheetTitle>
          <SheetDescription className="text-xs">{tt("costs.sheet_hint")}</SheetDescription>
        </SheetHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <div className="flex-1 space-y-4 overflow-y-auto px-4">
            {error && (
              <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
                {error}
              </div>
            )}
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.name")}</span>
              <Input
                required
                autoFocus
                className="h-9 text-sm"
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                placeholder={tt("costs.name")}
              />
            </label>
            <div className="grid grid-cols-[1fr_auto] gap-2">
              <label className="block space-y-1">
                <span className={FIELD_LABEL}>{tt("costs.amount")}</span>
                <Input
                  required
                  min="0.01"
                  step="0.01"
                  type="number"
                  className="h-9 text-sm"
                  value={form.amount}
                  onChange={(event) => setForm({ ...form, amount: event.target.value })}
                  placeholder={tt("costs.amount")}
                />
              </label>
              <label className="block space-y-1">
                <span className={FIELD_LABEL}>USD / RUB</span>
                <NativeSelect
                  value={form.currency}
                  className="h-9"
                  onChange={(event) => setForm({ ...form, currency: event.target.value as "USD" | "RUB" })}
                >
                  <NativeSelectOption value="USD">USD</NativeSelectOption>
                  <NativeSelectOption value="RUB">RUB</NativeSelectOption>
                </NativeSelect>
              </label>
            </div>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.category")}</span>
              <NativeSelect
                value={form.category}
                className="w-full"
                onChange={(event) => setForm({ ...form, category: event.target.value })}
              >
                {CATEGORIES.map((value) => (
                  <NativeSelectOption key={value} value={value}>
                    {tt(`costs.categories.${value}`)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.frequency")}</span>
              <NativeSelect
                value={form.frequency}
                className="w-full"
                onChange={(event) => setForm({ ...form, frequency: event.target.value as "monthly" | "yearly" })}
              >
                <NativeSelectOption value="monthly">{tt("costs.frequencies.monthly")}</NativeSelectOption>
                <NativeSelectOption value="yearly">{tt("costs.frequencies.yearly")}</NativeSelectOption>
              </NativeSelect>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.charge_day")}</span>
              <Input
                required
                min="1"
                max="31"
                type="number"
                className="h-9 text-sm"
                value={form.chargeDay}
                onChange={(event) => setForm({ ...form, chargeDay: event.target.value })}
              />
              <span className={FIELD_HINT}>
                {form.frequency === "yearly" ? tt("costs.charge_day_hint_yearly") : tt("costs.charge_day_hint_monthly")}
              </span>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>
                {form.frequency === "yearly" ? tt("costs.starts_on_yearly") : tt("costs.starts_on")}
              </span>
              <Input
                required
                type="month"
                className="h-9 text-sm"
                value={monthValue(form.startsOn)}
                onChange={(event) => setForm({
                  ...form,
                  startsOn: event.target.value ? `${event.target.value}-01` : "",
                })}
              />
              <span className={FIELD_HINT}>
                {form.frequency === "yearly" ? tt("costs.starts_on_hint_yearly") : tt("costs.starts_on_hint_monthly")}
              </span>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.ends_on")}</span>
              <Input
                type="month"
                className="h-9 text-sm"
                value={monthValue(form.endsOn)}
                onChange={(event) => setForm({
                  ...form,
                  endsOn: event.target.value ? `${event.target.value}-01` : "",
                })}
              />
              <span className={FIELD_HINT}>{tt("costs.ends_on_hint")}</span>
            </label>
            <div className="rounded-lg border bg-muted/30 px-3 py-2 text-xs">
              <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                {tt("costs.schedule")}
              </div>
              <div className="mt-1 text-sm text-foreground">{preview}</div>
            </div>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("costs.notes")}</span>
              <Input
                className="h-9 text-sm"
                value={form.notes}
                onChange={(event) => setForm({ ...form, notes: event.target.value })}
                placeholder={tt("costs.notes")}
              />
            </label>
          </div>
          <SheetFooter>
            <Button type="submit" disabled={busy} className="w-full">
              {editing ? tt("costs.save") : t(($) => $.actions.add_cost)}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
