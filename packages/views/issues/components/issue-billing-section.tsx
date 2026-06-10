"use client";

// Agency billing block on the issue detail sidebar (fork feature).
//
// Renders only when the issue has a price snapshot (a `client_billing_charge`
// row created on the done-transition). Shows the client-facing price with its
// derivation (no-cache USD x fx x markup) and lets staff confirm or void the
// draft. Guests never reach this surface (they have no web UI session), and
// the backend additionally rejects them.

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { issueKeys, issueBillingChargeOptions } from "@multica/core/issues/queries";
import type { ClientBillingCharge } from "@multica/core/types";
import { PropRow } from "../../common/prop-row";
import { useT } from "../../i18n";

function formatRub(value: number): string {
  return `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })} ₽`;
}

function formatUsd(value: number): string {
  return `$${value.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function StatusBadge({ status }: { status: ClientBillingCharge["status"] }) {
  const { t } = useT("issues");
  const styles: Record<ClientBillingCharge["status"], string> = {
    draft: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    confirmed: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
    void: "bg-muted text-muted-foreground line-through",
  };
  const labels: Record<ClientBillingCharge["status"], string> = {
    draft: t(($) => $.detail.billing_status_draft),
    confirmed: t(($) => $.detail.billing_status_confirmed),
    void: t(($) => $.detail.billing_status_void),
  };
  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${styles[status]}`}>
      {labels[status]}
    </span>
  );
}

export function IssueBillingSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const qc = useQueryClient();
  const { data: charge } = useQuery(issueBillingChargeOptions(issueId));

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: issueKeys.billingCharge(issueId) });

  const confirmMut = useMutation({
    mutationFn: () => api.confirmIssueBillingCharge(issueId),
    onSuccess: () => {
      toast.success(t(($) => $.detail.billing_confirmed_toast));
      invalidate();
    },
    onError: () => toast.error(t(($) => $.detail.billing_action_failed_toast)),
  });

  const voidMut = useMutation({
    mutationFn: () => api.voidIssueBillingCharge(issueId),
    onSuccess: () => {
      toast.success(t(($) => $.detail.billing_voided_toast));
      invalidate();
    },
    onError: () => toast.error(t(($) => $.detail.billing_action_failed_toast)),
  });

  if (!charge) return null;

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.detail.billing_section)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2">
          <div className="mb-1.5 flex items-center gap-2">
            <span className={`text-sm font-semibold ${charge.status === "void" ? "text-muted-foreground line-through" : ""}`}>
              {formatRub(charge.price_rub)}
            </span>
            <StatusBadge status={charge.status} />
          </div>
          <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
            <PropRow label={t(($) => $.detail.billing_prop_compute)}>
              <span className="text-muted-foreground">{formatUsd(charge.nocache_usd)}</span>
            </PropRow>
            <PropRow label={t(($) => $.detail.billing_prop_fx)}>
              <span className="text-muted-foreground">{charge.fx_rate.toLocaleString("ru-RU")}</span>
            </PropRow>
            <PropRow label={t(($) => $.detail.billing_prop_markup)}>
              <span className="text-muted-foreground">×{charge.markup}</span>
            </PropRow>
            {charge.adjusted_reason && (
              <PropRow label={t(($) => $.detail.billing_prop_adjusted)}>
                <span className="text-muted-foreground">{charge.adjusted_reason}</span>
              </PropRow>
            )}
          </div>
          {charge.status === "draft" && (
            <div className="mt-2 flex gap-2">
              <Button
                size="sm"
                variant="default"
                className="h-6 px-2 text-xs"
                disabled={confirmMut.isPending}
                onClick={() => confirmMut.mutate()}
              >
                {t(($) => $.detail.billing_confirm)}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs text-muted-foreground"
                disabled={voidMut.isPending}
                onClick={() => voidMut.mutate()}
              >
                {t(($) => $.detail.billing_void)}
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
