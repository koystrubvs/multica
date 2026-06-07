import { z } from "zod";

// Daily USD->RUB rate point from GET /api/fx/daily — the Bank of Russia (ЦБ РФ)
// daily feed, lazily mirrored server-side (see migration 118 / handler/fx.go).
// The response is a dense, ascending, carry-forward-filled series so every
// calendar date in the requested window carries a value.
export const FxRateSchema = z
  .object({
    date: z.string().default(""),
    usd_rub: z.number().default(0),
  })
  .loose();

export const FxRateListSchema = z.array(FxRateSchema);
export type FxRate = z.infer<typeof FxRateSchema>;

// Last-resort USD->RUB used only when the FX endpoint yields nothing (CBR
// unreachable AND the rate table still empty). Keeps cost figures in a sane
// ballpark instead of collapsing to zero — this is an internal cost-visibility
// number, not an invoice.
export const FALLBACK_RUB_PER_USD = 90;
