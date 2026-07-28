"use client";

import { useState } from "react";
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useT } from "../i18n";

const PAYMENT_CHANNELS = ["personal_card", "cash", "bank", "other"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

const FIELD_LABEL = "text-[11px] font-medium uppercase tracking-wide text-muted-foreground";

function rub(value: unknown): string {
  const amount = Number(value ?? 0);
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(Number.isFinite(amount) ? amount : 0);
}

// Payment dates are entered by hand and read back as calendar days, so the
// default has to be the owner's local day rather than the UTC one.
function todayISO(): string {
  const now = new Date();
  return new Date(now.getTime() - now.getTimezoneOffset() * 60_000).toISOString().slice(0, 10);
}

// Right-side drawer for settling a receivable that was paid past the business
// account. Bank receipts are reconciled from the statement instead, so this
// surface only opens for card and cash agreements.
export function BusinessReceivablePaymentSheet({ businessID, receivable, defaultChannel, onClose, onRecorded }: {
  businessID: string;
  receivable: BusinessRow | null;
  defaultChannel: string;
  onClose: () => void;
  onRecorded: () => Promise<void>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as TT;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const remaining = receivable
    ? Math.max(Number(receivable.planned_amount_rub ?? 0) - Number(receivable.paid_amount_rub ?? 0), 0)
    : 0;

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!receivable) return;
    const fd = new FormData(event.currentTarget);
    setBusy(true);
    setError("");
    try {
      await api.businessAction(businessID, `receivables/${String(receivable.id)}/payments`, {
        amount_rub: String(fd.get("amount") ?? ""),
        received_on: String(fd.get("received_on") ?? ""),
        payment_channel: String(fd.get("channel") ?? ""),
        notes: String(fd.get("notes") ?? ""),
        idempotency_key: crypto.randomUUID(),
      });
      await onRecorded();
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={!!receivable}
      onOpenChange={(next) => {
        if (!next) {
          setError("");
          onClose();
        }
      }}
    >
      <SheetContent side="right" className="w-full data-[side=right]:sm:max-w-md">
        {receivable && (
          <>
            <SheetHeader>
              <SheetTitle className="text-base">{t(($) => $.actions.record_payment)}</SheetTitle>
              <SheetDescription className="text-xs">
                {String(receivable.client_name ?? "")}
                {" · "}
                {String(receivable.period_key ?? "")}
                {" · "}
                {t(($) => $.money.outstanding)}: {rub(remaining)}
              </SheetDescription>
            </SheetHeader>
            <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
              <div className="flex-1 space-y-4 overflow-y-auto px-4">
                {error && (
                  <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
                    {error}
                  </div>
                )}
                <label className="block space-y-1">
                  <span className={FIELD_LABEL}>{t(($) => $.fields.amount)}</span>
                  <Input
                    required
                    autoFocus
                    type="number"
                    step="0.01"
                    min="0.01"
                    max={remaining}
                    name="amount"
                    defaultValue={remaining}
                    className="h-9 text-sm"
                  />
                </label>
                <label className="block space-y-1">
                  <span className={FIELD_LABEL}>{t(($) => $.fields.date)}</span>
                  <Input required type="date" name="received_on" defaultValue={todayISO()} className="h-9 text-sm" />
                </label>
                <label className="block space-y-1">
                  <span className={FIELD_LABEL}>{tt("columns.payment_channel", { defaultValue: "payment channel" })}</span>
                  <NativeSelect name="channel" defaultValue={defaultChannel} className="w-full">
                    {PAYMENT_CHANNELS.map((value) => (
                      <NativeSelectOption key={value} value={value}>
                        {tt(`values.${value}`, { defaultValue: value })}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </label>
                <label className="block space-y-1">
                  <span className={FIELD_LABEL}>{t(($) => $.fields.notes)}</span>
                  <Input name="notes" className="h-9 text-sm" placeholder={t(($) => $.fields.notes)} />
                </label>
                <p className="text-[11px] text-muted-foreground">{t(($) => $.money.payment_hint)}</p>
              </div>
              <SheetFooter>
                <Button type="submit" disabled={busy} className="w-full">
                  {t(($) => $.actions.record_payment)}
                </Button>
              </SheetFooter>
            </form>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
