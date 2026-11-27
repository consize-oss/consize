import { http, HttpResponse } from "msw";
import type { Configuration, Installation, PluginDetail } from "@/features/plugins/schemas";
import type { Scenario } from "./handlers";

// ── Seed catalog ───────────────────────────────────────────────────────────
type Seed = Omit<PluginDetail, "installation"> & { declared_access: string };

const CONSIZE = { name: "Consize maintained", maintained_by_consize: true };
const COMMUNITY = { name: "Community publisher", maintained_by_consize: false };

const SEEDS: Seed[] = [
  {
    id: "prometheus", name: "Prometheus Metrics", version: "0.3.0-alpha", package_label: "Built-in", brand: "prometheus",
    description: "Collect operational metrics for recommendations and post-change verification.",
    publisher: CONSIZE, capabilities: ["evidence"], availability: "built_in", declared_access: "Read-only",
    overview: "Reads utilization and restart signals used as evidence for every recommendation and to verify changes after they ship.",
    permissions: [{ id: "query", access: "read", title: "Query metrics", description: "Read CPU, memory, and restart series from the configured Prometheus." }],
    documentation_url: "https://docs.consize.example/plugins/prometheus", install_blocker: "Built into Consize, so there is nothing to install.", configurable: false,
  },
  {
    id: "kubernetes", name: "Kubernetes Action", version: "0.3.0-alpha", package_label: "Built-in", brand: "kubernetes",
    description: "Discover deployments and apply approved resource changes.",
    publisher: CONSIZE, capabilities: ["discovery", "actions"], availability: "built_in", declared_access: "Infrastructure write",
    overview: "Discovers Deployments with their requests and limits, and applies changes that have passed policy and approval.",
    permissions: [
      { id: "read", access: "read", title: "Read deployments", description: "List Deployments, their resource requests and limits, and rollout state." },
      { id: "write", access: "write", title: "Apply approved changes", description: "Patch resource requests and limits in permitted namespaces." },
    ],
    documentation_url: "https://docs.consize.example/plugins/kubernetes", install_blocker: "Built into Consize, so there is nothing to install.", configurable: false,
  },
  {
    id: "github", name: "GitHub Pull Requests", version: "0.3.0", package_label: "Signed package", brand: "github",
    description: "Propose infrastructure changes as reviewable pull requests in GitHub.",
    publisher: CONSIZE, capabilities: ["actions"], availability: "available", declared_access: "Repository write",
    overview: "Create a branch containing the proposed infrastructure change. Open a pull request with the plan and supporting evidence.",
    overview_note: "Configured infrastructure repositories",
    permissions: [
      { id: "meta", access: "read", title: "Read repository metadata", description: "Identify the selected repository and base branch." },
      { id: "pr", access: "write", title: "Write branches and pull requests", description: "Create a change branch and open a pull request in the configured repositories." },
    ],
    documentation_url: "https://docs.consize.example/plugins/github-pull-requests", install_blocker: null, configurable: true,
  },
  {
    id: "cloud-pricing", name: "Cloud Pricing", version: "0.3.0-alpha", package_label: "Built-in", brand: "pricing",
    description: "Enrich resource reductions with clearly sourced price estimates.",
    publisher: CONSIZE, capabilities: ["cost_billing"], availability: "built_in", declared_access: "Read-only",
    overview: "Attaches price estimates, with their source, to the savings shown on each recommendation.",
    permissions: [{ id: "prices", access: "read", title: "Read public price lists", description: "Fetch provider price lists; no account access." }],
    documentation_url: null, install_blocker: "Built into Consize, so there is nothing to install.", configurable: false,
  },
  {
    id: "datadog", name: "Datadog Metrics", version: "2.1.0", package_label: "Signed package", brand: "datadog",
    description: "Provide operational evidence from configured Datadog metrics.",
    publisher: COMMUNITY, capabilities: ["evidence"], availability: "requires_upgrade", declared_access: "Read-only",
    overview: "Supplies utilization and error-rate evidence from Datadog for recommendations and post-change checks.",
    permissions: [{ id: "query", access: "read", title: "Query metrics", description: "Read metric series from the configured Datadog account." }],
    documentation_url: "https://docs.consize.example/plugins/datadog",
    install_blocker: "This plugin needs a newer version of Consize. Upgrade Consize, then install it.", configurable: true,
  },
  {
    id: "webhook", name: "Webhook Notifications", version: "1.0.0", package_label: "Unsigned package", brand: "webhook",
    description: "Deliver workflow events to approved destinations.",
    publisher: COMMUNITY, capabilities: ["notifications"], availability: "verification_required", declared_access: "Send notifications",
    overview: "Posts approval requests and policy blocks to webhook endpoints you approve.",
    permissions: [{ id: "send", access: "write", title: "Send webhooks", description: "Send event payloads to approved destinations." }],
    documentation_url: null,
    install_blocker: "This package hasn't been verified. An administrator must verify its signature before it can be installed.", configurable: true,
  },
];

const ACCOUNTS: Configuration["accounts"] = [
  {
    id: "acme-platform", name: "Acme Platform",
    repositories: [
      { id: "acme-platform/terraform", full_name: "acme-platform/terraform" },
      { id: "acme-platform/kubernetes", full_name: "acme-platform/kubernetes" },
    ],
  },
  { id: "acme-payments", name: "Acme Payments", repositories: [{ id: "acme-payments/infrastructure", full_name: "acme-payments/infrastructure" }] },
];
const KNOWN_REPOS = new Set(ACCOUNTS.flatMap((a) => a.repositories.map((r) => r.id)));
const DEFAULT_VALUES: Configuration["values"] = { repository_ids: [], name: "Default", base_branch: "", infrastructure_path: "/", resource_mapping: "" };

// ── In-memory state (what a real backend would persist) ────────────────────
const installations = new Map<string, Installation>();
const configs = new Map<string, Configuration["values"]>();

export function resetPluginMock() {
  installations.clear();
  configs.clear();
  installations.set("prometheus", { status: "healthy", declared_access: "Read-only" });
  installations.set("kubernetes", { status: "healthy", declared_access: "Infrastructure write" });
}
resetPluginMock();

function installationOf(id: string, scenario: Scenario): Installation | null {
  const base = installations.get(id) ?? null;
  // "partial-failure" degrades Kubernetes to demonstrate the Degraded state.
  if (base && id === "kubernetes" && scenario === "partial-failure") {
    return { ...base, status: "degraded", status_reason: "API server latency elevated" };
  }
  return base;
}

const detailOf = (seed: Seed, scenario: Scenario): PluginDetail => {
  const { declared_access: _unused, ...rest } = seed;
  void _unused;
  return { ...rest, installation: installationOf(seed.id, scenario) };
};

const summaryOf = (seed: Seed, scenario: Scenario) => {
  const d = detailOf(seed, scenario);
  return {
    id: d.id, name: d.name, description: d.description, version: d.version, publisher: d.publisher,
    package_label: d.package_label, brand: d.brand, capabilities: d.capabilities,
    availability: d.availability, installation: d.installation,
  };
};

const problem = (status: number, title: string, code: string, detail?: string) =>
  HttpResponse.json({ title, status, code, detail }, { status, headers: { "Content-Type": "application/problem+json" } });

export function createPluginHandlers(getScenario: () => Scenario) {
  const down = () => (getScenario() === "backend-down" ? problem(503, "Plugin service unavailable", "backend_unavailable") : undefined);
  const find = (id: unknown) => SEEDS.find((s) => s.id === String(id));

  return [
    http.get("*/api/v1/plugins", () => down() ?? HttpResponse.json({ plugins: SEEDS.map((s) => summaryOf(s, getScenario())) })),

    http.get("*/api/v1/plugins/:id", ({ params }) => {
      const blocked = down();
      if (blocked) return blocked;
      const seed = find(params.id);
      return seed ? HttpResponse.json(detailOf(seed, getScenario())) : problem(404, "Plugin not found", "not_found");
    }),

    http.post("*/api/v1/plugins/:id/install", ({ params }) => {
      const blocked = down();
      if (blocked) return blocked;
      const seed = find(params.id);
      if (!seed) return problem(404, "Plugin not found", "not_found");
      if (installations.has(seed.id)) return problem(409, "Already installed", "already_installed");
      if (seed.install_blocker) return problem(422, "Can't install this plugin", "install_blocked", seed.install_blocker);
      installations.set(seed.id, {
        status: seed.configurable ? "configuration_required" : "healthy",
        declared_access: seed.declared_access,
      });
      return HttpResponse.json(detailOf(seed, getScenario()));
    }),

    http.get("*/api/v1/plugins/:id/configuration", ({ params }) => {
      const blocked = down();
      if (blocked) return blocked;
      const seed = find(params.id);
      if (!seed) return problem(404, "Plugin not found", "not_found");
      if (!seed.configurable || !installations.has(seed.id)) return problem(409, "Nothing to configure", "not_configurable");
      return HttpResponse.json({
        connect_url: "https://github.com/apps/consize-demo/installations/new",
        accounts: ACCOUNTS,
        values: configs.get(seed.id) ?? DEFAULT_VALUES,
      });
    }),

    http.put("*/api/v1/plugins/:id/configuration", async ({ params, request }) => {
      const blocked = down();
      if (blocked) return blocked;
      const seed = find(params.id);
      if (!seed) return problem(404, "Plugin not found", "not_found");
      if (!installations.has(seed.id)) return problem(409, "Install the plugin first", "not_installed");

      const body = (await request.json()) as Configuration["values"] & { enable: boolean };
      const unknown = body.repository_ids.filter((id) => !KNOWN_REPOS.has(id));
      if (unknown.length > 0) return problem(422, "Unknown repositories", "validation_failed", `Not connected: ${unknown.join(", ")}`);
      if (body.enable && body.repository_ids.length === 0) {
        return problem(422, "Can't enable yet", "validation_failed", "Select at least one repository to enable this plugin.");
      }
      const { enable, ...values } = body;
      configs.set(seed.id, values);
      installations.set(seed.id, {
        status: enable ? "connected" : "disabled",
        declared_access: seed.declared_access,
      });
      return HttpResponse.json(detailOf(seed, getScenario()));
    }),
  ];
}

// For tests that exercise the pure catalog model without HTTP.
export function pluginCatalogFixture(scenario: Scenario = "healthy") {
  return SEEDS.map((s) => summaryOf(s, scenario));
}