package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	// ComposioMCPApps gates the Composio app management UI and — together with
	// the MUL-3963 permission_mode / invocation_targets access model it depends
	// on — the aligned Private / Public-to picker in the agent create flow.
	// The access model exists to gate Composio sharing, so the two ship on the
	// same switch.
	ComposioMCPApps = "composio_mcp_apps"
	// AgentBuilder controls writes of system builder agents. It stays disabled
	// through the schema-only rollout so an older server cannot expose them.
	AgentBuilder = "agents_agent_builder"
	// ResourceLabels controls the agent- and skill-scoped label namespaces.
	// Issue labels remain available while this release flag is off.
	ResourceLabels = "settings_resource_labels"
	// BusinessControlPlane gates the W1 owner-only business account registry.
	// W2-W7 build on this parent switch and remain independently reversible.
	BusinessControlPlane        = "business_control_plane"
	BusinessClientsUI           = "business_clients_ui"
	BusinessCalendar            = "business_calendar"
	BusinessBankImport          = "business_bank_import"
	BusinessBankAPISync         = "business_bank_api_sync"
	BusinessTaskEconomicsShadow = "business_task_economics_shadow"
	BusinessTaskEconomicsAccept = "business_task_economics_accept"
	BusinessAccruals            = "business_accruals"
	BusinessPayoutBatches       = "business_payout_batches"
	ModulbankPayoutDrafts       = "modulbank_payout_drafts"
	BusinessDashboard           = "business_dashboard"
	BusinessAgentSummary        = "business_agent_summary"
	// agentSkillTogglesCompat is no longer a release flag. Keep publishing the
	// key as enabled so installed v0.4.0 desktop clients, which still gate the
	// switch on this config decision, receive the permanently enabled behavior.
	agentSkillTogglesCompat = "agents_skill_toggles"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
	AgentBuilder,
	ResourceLabels,
	BusinessControlPlane,
	BusinessClientsUI,
	BusinessCalendar,
	BusinessBankImport,
	BusinessTaskEconomicsShadow,
	BusinessTaskEconomicsAccept,
	BusinessAccruals,
	BusinessPayoutBatches,
	ModulbankPayoutDrafts,
	BusinessDashboard,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

func AgentBuilderEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, AgentBuilder, false)
}

func ResourceLabelsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ResourceLabels, false)
}

func BusinessControlPlaneEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, BusinessControlPlane, false)
}

func BusinessFeatureEnabled(ctx context.Context, flags *featureflag.Service, key string) bool {
	return BusinessControlPlaneEnabled(ctx, flags) && flags.IsEnabled(ctx, key, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags)+1)
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	out[agentSkillTogglesCompat] = true
	return out
}
