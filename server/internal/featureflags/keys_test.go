package featureflags

import (
	"context"
	"testing"
)

func TestResourceLabelsReleaseFlagDefaultsToOff(t *testing.T) {
	ctx := context.Background()
	if ResourceLabelsEnabled(ctx, nil) {
		t.Fatal("resource labels release flag must default to off")
	}
	if BusinessControlPlaneEnabled(ctx, nil) {
		t.Fatal("business control plane release flag must default to off")
	}
	for _, key := range []string{
		BusinessClientsUI,
		BusinessCalendar,
		BusinessBankImport,
		BusinessBankAPISync,
		BusinessTaskEconomicsShadow,
		BusinessTaskEconomicsAccept,
		BusinessAccruals,
		BusinessPayoutBatches,
		ModulbankPayoutDrafts,
		BusinessDashboard,
		BusinessAgentSummary,
	} {
		if BusinessFeatureEnabled(ctx, nil, key) {
			t.Fatalf("business feature %q must default to off", key)
		}
	}
}

func TestAgentBuilderCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentBuilderCompat] {
		t.Fatal("agent builder must stay enabled for installed clients")
	}
}

func TestAgentSkillTogglesCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentSkillTogglesCompat] {
		t.Fatal("agent skill toggles must stay enabled for installed v0.4.0 clients")
	}
}
