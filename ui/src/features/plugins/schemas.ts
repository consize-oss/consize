import { z } from "zod";

// ── Catalog contract ───────────────────────────────────────────────────────
// One list serves the Installed tab, the Catalog tab and the Overview
// integration strip, so they can never disagree about a plugin's state.
// The backend owns every user-facing sentence (publisher label, package label,
// declared access, blockers); the UI never derives them.

export const CAPABILITIES = ["discovery", "evidence", "actions", "cost_billing", "notifications"] as const;
export const capabilitySchema = z.enum(CAPABILITIES);
export type Capability = z.infer<typeof capabilitySchema>;

export const brandSchema = z.enum(["prometheus", "kubernetes", "github", "datadog", "webhook", "pricing", "generic"]);
export type Brand = z.infer<typeof brandSchema>;

export const installationSchema = z.object({
  status: z.enum(["healthy", "connected", "configuration_required", "disabled", "degraded", "failing"]),
  // Machine reason behind a degraded/failing state ("HTTP 401"), shown verbatim.
  status_reason: z.string().optional(),
  // What the plugin is allowed to do, in the backend's words ("Repository write").
  declared_access: z.string(),
});
export type Installation = z.infer<typeof installationSchema>;

export const pluginSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  version: z.string(),
  publisher: z.object({ name: z.string(), maintained_by_consize: z.boolean() }),
  package_label: z.string(), // "Built-in", "Signed package"
  brand: brandSchema,
  capabilities: z.array(capabilitySchema).min(1),
  availability: z.enum(["available", "built_in", "requires_upgrade", "verification_required"]),
  // null = not installed.
  installation: installationSchema.nullable(),
});
export type Plugin = z.infer<typeof pluginSchema>;

export const pluginListSchema = z.object({ plugins: z.array(pluginSchema) });

export const pluginDetailSchema = pluginSchema.extend({
  overview: z.string(),
  overview_note: z.string().optional(),
  permissions: z.array(
    z.object({
      id: z.string(),
      access: z.enum(["read", "write"]),
      title: z.string(),
      description: z.string(),
    }),
  ),
  documentation_url: z.string().url().nullable(),
  // Why this plugin can't be installed right now; null = it can.
  install_blocker: z.string().nullable(),
  // Whether installing it leaves required settings to fill in.
  configurable: z.boolean(),
});
export type PluginDetail = z.infer<typeof pluginDetailSchema>;

// ── Configuration (GET to open the dialog, PUT to save) ────────────────────
export const configurationSchema = z.object({
  // Where to link an account via the provider's own flow; null = none offered.
  connect_url: z.string().url().nullable(),
  accounts: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      repositories: z.array(z.object({ id: z.string(), full_name: z.string() })),
    }),
  ),
  values: z.object({
    repository_ids: z.array(z.string()),
    name: z.string(),
    base_branch: z.string(),
    infrastructure_path: z.string(),
    resource_mapping: z.string(),
  }),
});
export type Configuration = z.infer<typeof configurationSchema>;

export type ConfigurationInput = Configuration["values"] & { enable: boolean };

// ── Display vocabulary the UI owns (enums, not sentences) ──────────────────
export const CAPABILITY_LABELS: Record<Capability, string> = {
  discovery: "Discovery",
  evidence: "Evidence",
  actions: "Actions",
  cost_billing: "Cost & billing",
  notifications: "Notifications",
};