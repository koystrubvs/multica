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
import { api, ApiError } from "@multica/core/api";
import type {
  ClientBillingWorkspaceConfigUpdate,
  ContractorPeriodGroup,
} from "@multica/core/types";
import { useT } from "../../i18n";

const wsBillingKeys = {
  config: () => ["client-billing", "workspace-config"] as const,
  orgs: () => ["client-billing", "elba-orgs"] as const,
  accounts: (orgId: string) => ["client-billing", "elba-bank-accounts", orgId] as const,
};

// Elba list entities carry their human label in type-specific fields, not a
// uniform `name` — organizations expose only `inn`, bank accounts expose
// `accountNumber` + nested `bank.name`. Fall back to the id only as a last
// resort so the picker never shows a bare UUID.
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function elbaOrgLabel(o: { id: string; [k: string]: unknown }): string {
  const inn = str(o.inn);
  return str(o.name) || (inn ? `ИНН ${inn}` : o.id);
}
function elbaBankAccountLabel(a: { id: string; [k: string]: unknown }): string {
  const acc = str(a.accountNumber);
  if (acc) {
    const bank = a.bank as { name?: string } | undefined;
    return bank?.name ? `${acc} · ${bank.name}` : acc;
  }
  return str(a.name) || a.id;
}

function elbaContractorLabel(c: { id: string; [k: string]: unknown }): string {
  return str(c.name) || str(c.fullName) || str(c.shortName) || c.id;
}

const contractorKeys = {
  configs: () => ["client-billing", "contractor-configs"] as const,
  contractors: (orgId: string) => ["client-billing", "elba-contractors", orgId] as const,
  invoiceable: () => ["client-billing", "invoiceable-groups"] as const,
};

// ContractorBillingSection — contractor-level mode/fee + the consolidated
// «Выставить счёт» action (migration 202). One Elba contractor can span
// several projects; the invoice is issued for the contractor as a whole.
function ContractorBillingSection({ orgId }: { orgId: string }) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const { data: configs = [] } = useQuery({
    queryKey: contractorKeys.configs(),
    queryFn: () => api.listContractorBillingConfigs(),
  });
  const contractorsQuery = useQuery({
    queryKey: contractorKeys.contractors(orgId),
    queryFn: () => api.getElbaContractors(orgId),
    enabled: !!orgId,
    staleTime: 5 * 60_000,
    retry: false,
  });
  const { data: groups = [] } = useQuery({
    queryKey: contractorKeys.invoiceable(),
    queryFn: () => api.listInvoiceableContractorGroups(),
  });

  // Per-contractor draft state (mode + fee), seeded from saved configs.
  const [drafts, setDrafts] = useState<Record<string, { mode: string; fee: string }>>({});
  useEffect(() => {
    setDrafts((prev) => {
      const next = { ...prev };
      for (const c of configs) {
        if (!next[c.elba_contractor_id]) {
          next[c.elba_contractor_id] = {
            mode: c.mode,
            fee: c.subscription_fee_rub ? String(c.subscription_fee_rub) : "",
          };
        }
      }
      return next;
    });
  }, [configs]);

  const draftFor = (id: string) => drafts[id] ?? { mode: "postpaid", fee: "" };
  const setDraft = (id: string, patch: Partial<{ mode: string; fee: string }>) =>
    setDrafts((prev) => ({ ...prev, [id]: { ...draftFor(id), ...patch } }));

  const saveMut = useMutation({
    mutationFn: (v: { id: string; name: string; mode: string; fee: string }) =>
      api.upsertContractorBillingConfig({
        elba_contractor_id: v.id,
        name: v.name,
        mode: v.mode === "subscription" ? "subscription" : "postpaid",
        subscription_fee_rub: v.fee.trim() === "" ? 0 : Number(v.fee),
      }),
    onSuccess: () => {
      toast.success(t(($) => $.billing.contractor_saved_toast));
      qc.invalidateQueries({ queryKey: contractorKeys.configs() });
    },
    onError: () => toast.error(t(($) => $.billing.contractor_save_failed_toast)),
  });

  const invoiceMut = useMutation({
    mutationFn: (g: ContractorPeriodGroup) =>
      api.invoiceContractorPeriod(g.elba_contractor_id, g.starts_on, g.ends_on),
    onSuccess: (res) => {
      toast.success(
        t(($) => $.billing.invoice_success_toast, {
          amount: res.bill_rub.toLocaleString("ru-RU"),
          projects: res.period_count,
        }),
      );
      qc.invalidateQueries({ queryKey: contractorKeys.invoiceable() });
    },
    onError: (e) =>
      toast.error(e instanceof Error ? e.message : t(($) => $.billing.invoice_failed_toast)),
  });

  const selectClass =
    "h-8 w-full rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

  // Contractor id -> display name, from the Elba directory then saved configs.
  const nameFor = (id: string): string => {
    const fromElba = (contractorsQuery.data ?? []).find((c) => c.id === id);
    if (fromElba) return elbaContractorLabel(fromElba);
    const cfg = configs.find((c) => c.elba_contractor_id === id);
    return cfg?.name || id;
  };

  const fmt = (d: string) => d.slice(0, 10).split("-").reverse().join(".");

  return (
    <>
      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.billing.contractors_title)}</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t(($) => $.billing.contractors_description)}
        </p>
        {!orgId ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.billing.contractors_pick_org)}
          </p>
        ) : contractorsQuery.isError ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.billing.contractors_load_error)}
          </p>
        ) : (contractorsQuery.data ?? []).length === 0 ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.billing.contractors_none)}
          </p>
        ) : (
          <div className="mt-4 space-y-3">
            {(contractorsQuery.data ?? []).map((c) => {
              const d = draftFor(c.id);
              return (
                <div
                  key={c.id}
                  className="flex flex-wrap items-end gap-3 rounded-md border border-border p-3"
                >
                  <div className="min-w-40 flex-1 text-sm font-medium">
                    {elbaContractorLabel(c)}
                  </div>
                  <label className="flex flex-col gap-1 text-xs">
                    <span className="font-medium">{t(($) => $.billing.contractor_mode)}</span>
                    <select
                      value={d.mode}
                      onChange={(e) => setDraft(c.id, { mode: e.target.value })}
                      className={selectClass}
                    >
                      <option value="postpaid">{t(($) => $.billing.contractor_mode_postpaid)}</option>
                      <option value="subscription">
                        {t(($) => $.billing.contractor_mode_subscription)}
                      </option>
                    </select>
                  </label>
                  <label className="flex flex-col gap-1 text-xs">
                    <span className="font-medium">{t(($) => $.billing.contractor_fee)}</span>
                    <Input
                      type="number"
                      step="1000"
                      value={d.fee}
                      disabled={d.mode !== "subscription"}
                      onChange={(e) => setDraft(c.id, { fee: e.target.value })}
                      className="h-8 max-w-40"
                    />
                  </label>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={saveMut.isPending}
                    onClick={() =>
                      saveMut.mutate({
                        id: c.id,
                        name: elbaContractorLabel(c),
                        mode: d.mode,
                        fee: d.fee,
                      })
                    }
                  >
                    {t(($) => $.billing.contractor_save)}
                  </Button>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div>
        <h2 className="text-sm font-semibold">{t(($) => $.billing.invoiceable_title)}</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t(($) => $.billing.invoiceable_description)}
        </p>
        {groups.length === 0 ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.billing.invoiceable_none)}
          </p>
        ) : (
          <div className="mt-4 space-y-3">
            {groups.map((g) => (
              <div
                key={`${g.elba_contractor_id}-${g.starts_on}-${g.ends_on}`}
                className="rounded-md border border-border p-3"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="text-sm font-medium">
                    {nameFor(g.elba_contractor_id)}
                    <span className="ml-2 text-xs font-normal text-muted-foreground">
                      {`${fmt(g.starts_on)} — ${fmt(g.ends_on)}`}
                    </span>
                  </div>
                  <Button
                    size="sm"
                    disabled={invoiceMut.isPending}
                    onClick={() => invoiceMut.mutate(g)}
                  >
                    {t(($) => $.billing.invoice_button, {
                      amount: g.total_rub.toLocaleString("ru-RU"),
                    })}
                  </Button>
                </div>
                <ul className="mt-2 space-y-0.5 text-xs text-muted-foreground">
                  {g.projects.map((p) => (
                    <li key={p.period_id} className="flex justify-between">
                      <span>{p.project_title}</span>
                      <span>{`${p.total_rub.toLocaleString("ru-RU")} ₽`}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}

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
    "h-8 w-full rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50";

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
            {/* 503 = ELBA_API_KEY missing on the server; anything else is a
                live upstream failure — show it instead of blaming the key. */}
            {orgsQuery.error instanceof ApiError && orgsQuery.error.status === 503
              ? t(($) => $.billing.elba_unavailable)
              : t(($) => $.billing.elba_error, {
                  error: orgsQuery.error instanceof Error ? orgsQuery.error.message : "",
                })}
          </p>
        ) : (
          <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
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
                    {elbaOrgLabel(o)}
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
                    {elbaBankAccountLabel(a)}
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

      <ContractorBillingSection orgId={orgId} />
    </div>
  );
}
