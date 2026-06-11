"use client";

// Workspace billing settings (fork feature): the pricing defaults every
// project inherits (markup K, fx markup, per-task floor, rounding step) and
// the Kontur Elba wiring (organization + default bank account). Projects
// can override the pricing knobs individually; Elba contractors are linked
// per project on the project page.

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { api } from "@multica/core/api";
import type { ClientBillingWorkspaceConfigUpdate } from "@multica/core/types";
import { useT } from "../../i18n";

const wsBillingKeys = {
  config: () => ["client-billing", "workspace-config"] as const,
  orgs: () => ["client-billing", "elba-orgs"] as const,
  accounts: (orgId: string) => ["client-billing", "elba-bank-accounts", orgId] as const,
};

function Field({
  label,
  hint,
  value,
  onChange,
  step = "any",
}: {
  label: string;
  hint?: string;
  value: string;
  onChange: (v: string) => void;
  step?: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="text-xs font-medium">{label}</span>
      <Input type="number" step={step} value={value} onChange={(e) => onChange(e.target.value)} className="h-8 max-w-48" />
      {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
    </label>
  );
}

export function BillingTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const { data: config } = useQuery({
    queryKey: wsBillingKeys.config(),
    queryFn: () => api.getWorkspaceBillingConfig(),
  });

  const [markup, setMarkup] = useState("");
  const [minPrice, setMinPrice] = useState("");
  const [rounding, setRounding] = useState("");
  const [fxMarkup, setFxMarkup] = useState("");
  const [orgId, setOrgId] = useState("");
  const [bankAccountId, setBankAccountId] = useState("");

  useEffect(() => {
    if (!config) return;
    setMarkup(String(config.markup));
    setMinPrice(String(config.min_price_rub));
    setRounding(String(config.rounding_rub));
    setFxMarkup(String(config.fx_markup_percent));
    setOrgId(config.elba_org_id ?? "");
    setBankAccountId(config.elba_bank_account_id ?? "");
  }, [config]);

  // Elba pickers: 503 = ELBA_API_KEY not set — render the hint, don't retry.
  const orgsQuery = useQuery({
    queryKey: wsBillingKeys.orgs(),
    queryFn: () => api.getElbaOrganizations(),
    staleTime: 5 * 60_000,
    retry: false,
  });
  const { data: accounts = [] } = useQuery({
    queryKey: wsBillingKeys.accounts(orgId),
    queryFn: () => api.getElbaBankAccounts(orgId),
    enabled: !!orgId,
    staleTime: 5 * 60_000,
    retry: false,
  });

  const saveMut = useMutation({
    mutationFn: (update: ClientBillingWorkspaceConfigUpdate) =>
      api.putWorkspaceBillingConfig(update),
    onSuccess: () => {
      toast.success(t(($) => $.billing.saved_toast));
      qc.invalidateQueries({ queryKey: wsBillingKeys.config() });
    },
    onError: () => toast.error(t(($) => $.billing.save_failed_toast)),
  });

  const num = (s: string, fallback: number): number => {
    const n = Number(s);
    return s.trim() !== "" && Number.isFinite(n) ? n : fallback;
  };

  const handleSave = () => {
    saveMut.mutate({
      markup: num(markup, 3),
      min_price_rub: num(minPrice, 500),
      rounding_rub: num(rounding, 50),
      fx_markup_percent: num(fxMarkup, 5),
      elba_org_id: orgId,
      elba_bank_account_id: bankAccountId,
    });
  };

  const selectClass =
    "h-8 max-w-72 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

  return (
    <div className="max-w-2xl space-y-8">
      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.billing.pricing_title)}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.billing.pricing_description)}</p>
        <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field
            label={t(($) => $.billing.markup)}
            hint={t(($) => $.billing.markup_hint)}
            value={markup}
            onChange={setMarkup}
            step="0.1"
          />
          <Field
            label={t(($) => $.billing.fx_markup)}
            hint={t(($) => $.billing.fx_markup_hint)}
            value={fxMarkup}
            onChange={setFxMarkup}
            step="0.5"
          />
          <Field
            label={t(($) => $.billing.min_price)}
            value={minPrice}
            onChange={setMinPrice}
            step="50"
          />
          <Field
            label={t(($) => $.billing.rounding)}
            value={rounding}
            onChange={setRounding}
            step="10"
          />
        </div>
      </div>

      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.billing.elba_title)}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.billing.elba_description)}</p>
        {orgsQuery.isError ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.billing.elba_unavailable)}
          </p>
        ) : (
          <div className="mt-4 flex flex-col gap-4">
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs font-medium">{t(($) => $.billing.elba_org)}</span>
              <select
                value={orgId}
                onChange={(e) => {
                  setOrgId(e.target.value);
                  setBankAccountId("");
                }}
                className={selectClass}
              >
                <option value="">{t(($) => $.billing.elba_org_none)}</option>
                {(orgsQuery.data ?? []).map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name ?? o.id}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-xs font-medium">{t(($) => $.billing.elba_bank_account)}</span>
              <select
                value={bankAccountId}
                onChange={(e) => setBankAccountId(e.target.value)}
                className={selectClass}
                disabled={!orgId}
              >
                <option value="">{t(($) => $.billing.elba_bank_account_none)}</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name ?? a.id}
                  </option>
                ))}
              </select>
            </label>
          </div>
        )}
      </div>

      <Button size="sm" disabled={saveMut.isPending} onClick={handleSave}>
        {t(($) => $.billing.save)}
      </Button>
    </div>
  );
}
