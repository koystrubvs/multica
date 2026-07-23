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
import { cn } from "@multica/ui/lib/utils";
import { Download, Trash2 } from "lucide-react";
import { useT } from "../i18n";

const CONTRACT_STATUSES = ["draft", "active", "expired", "terminated"] as const;

type TT = (key: string, options?: { defaultValue?: string }) => string;

const FIELD_LABEL = "text-[11px] font-medium uppercase tracking-wide text-muted-foreground";

function text(row: BusinessRow, key: string): string {
  const value = row[key];
  return value === null || value === undefined ? "" : String(value);
}

function rub(value: unknown): string {
  const amount = Number(value ?? 0);
  if (!Number.isFinite(amount) || amount === 0) return "—";
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(amount);
}

function statusTone(status: string): string {
  if (status === "active") return "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
  if (status === "expired" || status === "terminated") return "bg-destructive/10 text-destructive";
  return "bg-muted text-muted-foreground";
}

export function BusinessClientContracts({ businessID, clientID, contracts, locale, onChanged }: {
  businessID: string;
  clientID: string;
  contracts: BusinessRow[];
  locale: string;
  onChanged: () => Promise<void>;
}) {
  const { t } = useT("business");
  const tt = t as unknown as TT;
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const run = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key);
    setError("");
    try {
      await action();
      await onChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy("");
    }
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const fd = new FormData(form);
    const file = fd.get("file");
    void run("create", async () => {
      let attachmentID = "";
      if (file instanceof File && file.size > 0) {
        const uploaded = await api.uploadFile(file);
        attachmentID = uploaded.id;
      }
      await api.businessAction(businessID, `clients/${clientID}/contracts`, {
        number: String(fd.get("number") ?? "").trim(),
        subject: String(fd.get("subject") ?? "").trim(),
        amount_rub: String(fd.get("amount") ?? "").trim() || null,
        starts_on: String(fd.get("starts_on") ?? "") || null,
        ends_on: String(fd.get("ends_on") ?? "") || null,
        status: fd.get("status"),
        attachment_id: attachmentID || null,
      });
      form.reset();
    });
  };

  const dateStr = (value: string): string => {
    if (!value) return "";
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleDateString(locale);
  };

  return (
    <div className="space-y-4">
      {error && <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">{error}</div>}

      <div className="space-y-2">
        {contracts.length === 0 && (
          <div className="rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">
            {tt("contracts.empty", { defaultValue: "No contracts yet" })}
          </div>
        )}
        {contracts.map((contract) => {
          const id = text(contract, "id");
          const status = text(contract, "status");
          const attachmentID = text(contract, "attachment_id");
          const fileName = text(contract, "file_name");
          const period = [dateStr(text(contract, "starts_on")), dateStr(text(contract, "ends_on"))].filter(Boolean).join(" — ");
          return (
            <div key={id} className="space-y-1.5 rounded-lg border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium">{text(contract, "number") || tt("contracts.tab", { defaultValue: "Contract" })}</span>
                <span className={cn("inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[11px] font-medium", statusTone(status))}>
                  {tt(`contracts.st_${status}`, { defaultValue: status })}
                </span>
                {Number(contract.amount_rub) > 0 && <span className="text-xs tabular-nums text-muted-foreground">{rub(contract.amount_rub)}</span>}
                <div className="ml-auto flex items-center gap-1">
                  {attachmentID && fileName && (
                    <a
                      href={`${api.getBaseUrl()}/api/attachments/${attachmentID}/download`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 rounded p-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      title={fileName}
                    >
                      <Download className="size-3.5" />
                      <span className="max-w-40 truncate">{tt("contracts.download", { defaultValue: "Download" })}</span>
                    </a>
                  )}
                  <button
                    type="button"
                    onClick={() => run(`del-${id}`, () => api.businessAction(businessID, `contracts/${id}`, undefined, "DELETE"))}
                    disabled={busy !== ""}
                    className="inline-flex items-center rounded p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
                    aria-label={tt("contracts.delete", { defaultValue: "Delete" })}
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </div>
              </div>
              {text(contract, "subject") && <div className="text-xs text-muted-foreground">{text(contract, "subject")}</div>}
              {period && <div className="text-[11px] tabular-nums text-muted-foreground">{period}</div>}
            </div>
          );
        })}
      </div>

      <form className="space-y-3 rounded-lg border border-dashed p-3" onSubmit={submit}>
        <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{tt("contracts.add", { defaultValue: "Add contract" })}</div>
        <div className="grid grid-cols-2 gap-2">
          <label className="space-y-1">
            <span className={FIELD_LABEL}>{tt("contracts.number", { defaultValue: "Number" })}</span>
            <Input name="number" className="h-8 text-xs" />
          </label>
          <label className="space-y-1">
            <span className={FIELD_LABEL}>{tt("columns.status", { defaultValue: "status" })}</span>
            <NativeSelect size="sm" name="status" defaultValue="active" className="w-full">
              {CONTRACT_STATUSES.map((value) => (
                <NativeSelectOption key={value} value={value}>{tt(`contracts.st_${value}`, { defaultValue: value })}</NativeSelectOption>
              ))}
            </NativeSelect>
          </label>
          <label className="col-span-2 space-y-1">
            <span className={FIELD_LABEL}>{tt("contracts.subject", { defaultValue: "Subject" })}</span>
            <Input name="subject" className="h-8 text-xs" />
          </label>
          <label className="space-y-1">
            <span className={FIELD_LABEL}>{tt("contracts.amount", { defaultValue: "Amount" })}</span>
            <Input name="amount" inputMode="decimal" className="h-8 text-xs" />
          </label>
          <div className="grid grid-cols-2 gap-2">
            <label className="space-y-1">
              <span className={FIELD_LABEL}>{tt("contracts.starts_on", { defaultValue: "Start" })}</span>
              <Input type="date" name="starts_on" className="h-8 text-xs" />
            </label>
            <label className="space-y-1">
              <span className={FIELD_LABEL}>{tt("contracts.ends_on", { defaultValue: "End" })}</span>
              <Input type="date" name="ends_on" className="h-8 text-xs" />
            </label>
          </div>
          <label className="col-span-2 space-y-1">
            <span className={FIELD_LABEL}>{tt("contracts.file", { defaultValue: "Contract file" })}</span>
            <Input type="file" name="file" className="h-8 cursor-pointer text-xs file:mr-2 file:text-xs" />
          </label>
        </div>
        <Button type="submit" size="sm" className="h-8 w-full" disabled={busy !== ""}>
          {tt("contracts.add", { defaultValue: "Add contract" })}
        </Button>
      </form>
    </div>
  );
}
