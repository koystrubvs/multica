"use client";

// Agency billing block on the issue detail sidebar (fork feature).
//
// Metering v2 (migration 134): renders the LIVE cost of the issue in any
// status — accrued-but-unbilled amount, already-billed total, and the charge
// ledger. The workspace owner additionally sees the internal cache-discounted
// cost (`internal` is present in the payload only for them — the backend
// decides, the client never even receives the margin otherwise).
//
// The "Оспорить" action files a billing dispute (members and guests alike);
// staff resolve it from the project Billing tab. Draft ledger lines keep the
// per-line confirm/void controls for manual review before period close.

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { issueKeys, issueBillingCostOptions } from "@multica/core/issues/queries";
import type { ClientBillingChargeStatus } from "@multica/core/types";
import { PropRow } from "../../common/prop-row";
import { useT } from "../../i18n";

function formatRub(value: number): string {
  return `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })} ₽`;
}

function formatUsd(value: number): string {
  return `$${value.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function StatusBadge({ status }: { status: ClientBillingChargeStatus }) {
  const { t } = useT("issues");
  const styles: Record<ClientBillingChargeStatus, string> = {
    draft: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    confirmed: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
    void: "bg-muted text-muted-foreground line-through",
  };
  const labels: Record<ClientBillingChargeStatus, string> = {
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
  const [disputeFormOpen, setDisputeFormOpen] = useState(false);
  const [disputeReason, setDisputeReason] = useState("");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  // Money is owner/admin. An employee keeps the Token usage section above this
  // one — token counts are a different endpoint and stay open to members. The
  // query is skipped rather than merely hidden: the backend answers 403 and
  // there is nothing here to render.
  const isBillingStaff = role === "owner" || role === "admin";
  const { data: cost } = useQuery({
    ...issueBillingCostOptions(issueId),
    enabled: isBillingStaff,
  });

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: issueKeys.billingCost(issueId) });

  const confirmMut = useMutation({
    mutationFn: (chargeId: string) => api.confirmIssueBillingCharge(issueId, chargeId),
    onSuccess: () => {
      toast.success(t(($) => $.detail.billing_confirmed_toast));
      invalidate();
    },
    onError: () => toast.error(t(($) => $.detail.billing_action_failed_toast)),
  });

  const voidMut = useMutation({
    mutationFn: (chargeId: string) => api.voidIssueBillingCharge(issueId, chargeId),
    onSuccess: () => {
      toast.success(t(($) => $.detail.billing_voided_toast));
      invalidate();
    },
    onError: () => toast.error(t(($) => $.detail.billing_action_failed_toast)),
  });

  const disputeMut = useMutation({
    mutationFn: (reason: string) => api.openIssueBillingDispute(issueId, reason),
    onSuccess: () => {
      toast.success(t(($) => $.detail.billing_dispute_opened_toast));
      setDisputeFormOpen(false);
      setDisputeReason("");
      invalidate();
    },
    onError: () => toast.error(t(($) => $.detail.billing_action_failed_toast)),
  });

  if (!isBillingStaff) return null;
  // Nothing to show: billing off AND the issue never burned a token.
  if (!cost || (!cost.billing_enabled && cost.total_tokens === 0)) return null;

  const headline = cost.unbilled_estimate_rub > 0 ? cost.unbilled_estimate_rub : cost.estimate_rub;

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
            <span className="text-sm font-semibold">{formatRub(headline)}</span>
            {cost.dispute && (
              <span className="inline-flex items-center rounded bg-red-500/15 px-1.5 py-0.5 text-[11px] font-medium text-red-600 dark:text-red-400">
                {t(($) => $.detail.billing_dispute_active)}
              </span>
            )}
          </div>
          <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
            {cost.unbilled_estimate_rub > 0 && (
              <PropRow label={t(($) => $.detail.billing_unbilled)}>
                <span className="text-muted-foreground">{formatRub(cost.unbilled_estimate_rub)}</span>
              </PropRow>
            )}
            {cost.billed_rub > 0 && (
              <PropRow label={t(($) => $.detail.billing_billed)}>
                <span className="text-muted-foreground">{formatRub(cost.billed_rub)}</span>
              </PropRow>
            )}
            <PropRow label={t(($) => $.detail.billing_prop_compute)}>
              <span className="text-muted-foreground">{formatUsd(cost.nocache_usd)}</span>
            </PropRow>
            <PropRow label={t(($) => $.detail.billing_prop_fx)}>
              <span className="text-muted-foreground">{cost.fx_rate.toLocaleString("ru-RU")}</span>
            </PropRow>
            {cost.markup !== 1 && (
              <PropRow label={t(($) => $.detail.billing_prop_markup)}>
                <span className="text-muted-foreground">×{cost.markup}</span>
              </PropRow>
            )}
            {cost.internal && (
              <PropRow label={t(($) => $.detail.billing_internal)}>
                <span className="text-muted-foreground">
                  {formatRub(cost.internal.rub)} ({formatUsd(cost.internal.usd)})
                </span>
              </PropRow>
            )}
          </div>

          {cost.charges.length > 0 && (
            <div className="mt-2 space-y-1.5">
              {cost.charges.map((c) => (
                <div key={c.id} className="rounded-md border border-border/40 px-2 py-1.5">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
                    <span className={`font-medium ${c.status === "void" ? "text-muted-foreground line-through" : ""}`}>
                      {formatRub(c.price_rub)}
                    </span>
                    <StatusBadge status={c.status} />
                    <span className="text-muted-foreground whitespace-nowrap">
                      {new Date(c.created_at).toLocaleDateString("ru-RU")}
                    </span>
                  </div>
                  {c.adjusted_reason && (
                    <div className="mt-0.5 text-[11px] text-muted-foreground" title={c.adjusted_reason}>
                      {c.adjusted_reason}
                    </div>
                  )}
                  {c.status === "draft" && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-5 px-1.5 text-[11px]"
                        disabled={confirmMut.isPending}
                        onClick={() => confirmMut.mutate(c.id)}
                      >
                        {t(($) => $.detail.billing_confirm)}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-5 px-1.5 text-[11px] text-muted-foreground"
                        disabled={voidMut.isPending}
                        onClick={() => voidMut.mutate(c.id)}
                      >
                        {t(($) => $.detail.billing_void)}
                      </Button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {cost.dispute ? (
            <div className="mt-2 rounded-md bg-red-500/10 px-2 py-1.5 text-xs text-red-700 dark:text-red-300">
              {cost.dispute.reason}
            </div>
          ) : disputeFormOpen ? (
            <div className="mt-2 space-y-1.5">
              <Textarea
                value={disputeReason}
                onChange={(e) => setDisputeReason(e.target.value)}
                placeholder={t(($) => $.detail.billing_dispute_reason_ph)}
                className="min-h-14 text-xs"
              />
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="default"
                  className="h-6 px-2 text-xs"
                  disabled={disputeMut.isPending || disputeReason.trim() === ""}
                  onClick={() => disputeMut.mutate(disputeReason.trim())}
                >
                  {t(($) => $.detail.billing_dispute_submit)}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-xs text-muted-foreground"
                  onClick={() => setDisputeFormOpen(false)}
                >
                  {t(($) => $.detail.billing_dispute_cancel)}
                </Button>
              </div>
            </div>
          ) : (
            (cost.unbilled_estimate_rub > 0 || cost.billed_rub > 0) && (
              <div className="mt-2">
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-xs text-muted-foreground"
                  onClick={() => setDisputeFormOpen(true)}
                >
                  {t(($) => $.detail.billing_dispute_open)}
                </Button>
              </div>
            )
          )}
        </div>
      )}
    </div>
  );
}
