"use client";

import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "@multica/core/api";
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

const CLIENT_STATUSES = ["active", "prospect", "paused", "leaving", "lost"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

const FIELD_LABEL = "text-[11px] font-medium uppercase tracking-wide text-muted-foreground";

// Right-side drawer for creating a new business client. Mirrors the
// BusinessClientCard Sheet so the two client surfaces read as one pattern.
// Only the core fields live here (name, status, payment channel, notes) —
// agreements, payers and projects are attached afterwards from the client card.
export function BusinessClientCreateSheet({ businessID, open, onOpenChange, onCreated }: {
  businessID: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (clientID: string) => Promise<void>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as TT;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const fd = new FormData(event.currentTarget);
    const name = String(fd.get("name") ?? "").trim();
    if (!name) return;
    setBusy(true);
    setError("");
    try {
      const created = await api.businessAction<{ id?: string }>(businessID, "clients", {
        canonical_name: name,
        status: fd.get("status"),
        primary_payment_channel: fd.get("channel"),
        notes: String(fd.get("notes") ?? "").trim() || null,
      });
      onOpenChange(false);
      await onCreated(String(created?.id ?? ""));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
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
          <SheetTitle className="text-base">{t(($) => $.actions.add_client)}</SheetTitle>
          <SheetDescription className="text-xs">{t(($) => $.card.hint)}</SheetDescription>
        </SheetHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <div className="flex-1 space-y-4 overflow-y-auto px-4">
            {error && (
              <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
                {error}
              </div>
            )}
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{t(($) => $.fields.name)}</span>
              <Input required autoFocus name="name" className="h-9 text-sm" placeholder={t(($) => $.fields.name)} />
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("columns.status", { defaultValue: "status" })}</span>
              <NativeSelect name="status" defaultValue="prospect" className="w-full">
                {CLIENT_STATUSES.map((value) => (
                  <NativeSelectOption key={value} value={value}>
                    {tt(`values.${value}`, { defaultValue: value })}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{tt("columns.primary_payment_channel", { defaultValue: "payment channel" })}</span>
              <NativeSelect name="channel" defaultValue="bank" className="w-full">
                <NativeSelectOption value="bank">{tt("values.bank", { defaultValue: "bank" })}</NativeSelectOption>
                <NativeSelectOption value="personal_card">{tt("values.personal_card", { defaultValue: "personal_card" })}</NativeSelectOption>
              </NativeSelect>
            </label>
            <label className="block space-y-1">
              <span className={FIELD_LABEL}>{t(($) => $.fields.notes)}</span>
              <Input name="notes" className="h-9 text-sm" placeholder={t(($) => $.fields.notes)} />
            </label>
          </div>
          <SheetFooter>
            <Button type="submit" disabled={busy} className="w-full">
              {t(($) => $.actions.add_client)}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
