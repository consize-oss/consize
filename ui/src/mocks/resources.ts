import type { components } from "../../lib/generated/api";
import type { ResourceInventoryDetails } from "@/features/resources/resourceInventory";
import { baseRecommendations } from "./fixtures";

const now = Date.now();
const observedAt = new Date(now - 2 * 60_000).toISOString();
const firstSeenAt = new Date(now - 30 * 24 * 60 * 60_000).toISOString();
const checkedAgo = (minutes: number) => new Date(now - minutes * 60_000).toISOString();

type Resource = components["schemas"]["Resource"];

const SCOPE_TYPE_LABELS: Record<string, string> = {
  "kubernetes.deployment": "Deployment",
  "aws.rds.instance": "RDS instance",
  "storage.bucket": "S3 bucket",
  "llm.application": "LLM router",
  "aws.elasticache.cluster": "ElastiCache cluster",
};

type ResourceSeed = Pick<Resource, "id" | "name" | "provider" | "environment" | "owner" | "region"> & {
  source: string;
  scopeType?: string;
  kind: NonNullable<ResourceInventoryDetails["kind"]>;
  labels?: Record<string, string>;
};

// A recommendation ID reuses its resource facts. An explicit seed creates a resource without adding advice.
// Legacy demo environments (including DataLake) are preserved; these are not registry registration inputs.
function mockResource(
  reference: string | ResourceSeed,
  type: string,
  supportStatus: Resource["support_status"],
  details: ResourceInventoryDetails,
): Resource {
  let resource: ResourceSeed;
  if (typeof reference === "string") {
    const recommendation = baseRecommendations.find((item) => item.id === reference);
    if (!recommendation) throw new Error("Missing mock recommendation: " + reference);
    const existing = recommendation.resource;
    resource = {
      id: existing.id,
      name: existing.name,
      provider: existing.provider,
      environment: existing.environment,
      owner: recommendation.owner,
      region: existing.region ?? "global",
      source: existing.source,
      scopeType: existing.type,
      kind: existing.kind ?? "compute",
      labels: { ...existing.tags, ...(existing.namespace ? { namespace: existing.namespace } : {}) },
    };
  } else {
    resource = reference;
  }

  const recommendationIds = baseRecommendations
    .filter((item) => item.resource.id === resource.id && !["executed", "superseded"].includes(item.status))
    .map((item) => item.id);

  return {
    schema_version: 1,
    id: resource.id,
    type,
    provider: resource.provider,
    provider_resource_id: "demo:" + resource.id,
    name: resource.name,
    environment: resource.environment,
    owner: resource.owner,
    region: resource.region,
    account: resource.source,
    criticality: "medium",
    lifecycle_state: "active",
    support_status: supportStatus,
    labels: resource.labels ?? {},
    metadata: {
      demo_inventory: {
        ...details, kind: resource.kind, recommendation_ids: recommendationIds,
        scope_type: resource.scopeType ?? SCOPE_TYPE_LABELS[type] ?? (type === "kubernetes.node_group" ? (resource.kind === "gpu" ? "GPU node group" : "Worker pool") : type),
      },
    },
    current_state: {},
    monthly_cost: 0,
    source_plugin_id: resource.source,
    observed_at: observedAt,
    first_seen_at: firstSeenAt,
    last_seen_at: observedAt,
    created_at: firstSeenAt,
    updated_at: observedAt,
  };
}

// Telemetry and coverage below are illustrative samples, not backend measurements.
export const baseResources: Resource[] = [
  mockResource("rec_01", "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · payments",
    owner_note: "core-pay-sre",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(0.5) },
    policy_coverage: "covered",
  }),
  mockResource("rec_02", "aws.rds.instance", "model_only", {
    description: "Amazon RDS · Aurora PostgreSQL",
    owner_note: "data-infra",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource("rec_05", "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · 42 TB",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(5) },
    policy_coverage: "unknown",
  }),
  mockResource("rec_07", "llm.application", "model_only", {
    description: "LLM Router · Bedrock and OpenAI",
    owner_note: "genai-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "OTel Gateway", checked_at: checkedAgo(180) },
    policy_coverage: "covered",
  }),
  mockResource("rec_06", "kubernetes.node_group", "unsupported", {
    description: "Kubernetes Deployment · staging-workers",
    owner_note: "qa-devops",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(0.75) },
    policy_coverage: "covered",
  }),
  mockResource("rec_08", "kubernetes.node_group", "unsupported", {
    description: "Amazon EKS Node Group · GPU",
    owner_note: "ml-ops",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource("rec_03", "kubernetes.node_group", "unsupported", {
    description: "Kubernetes GPU node group · inference",
    owner_note: "ml-ops",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource("rec_04", "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · media",
    owner_note: "media-platform",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource("rec_09", "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · checkout",
    owner_note: "checkout-sre",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(0.5) },
    policy_coverage: "covered",
  }),
  mockResource("rec_10", "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · messaging",
    owner_note: "messaging-sre",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource("rec_11", "aws.elasticache.cluster", "unsupported", {
    description: "Amazon ElastiCache · Redis sessions",
    owner_note: "core-pay-sre",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource("rec_12", "kubernetes.node_group", "unsupported", {
    description: "Kubernetes node group · CI runners",
    owner_note: "developer-platform",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource("rec_13", "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · access log archive",
    owner_note: "data-platform",
    optimization_status: "recommendations", action_path_available: true,
    telemetry: { status: "stale", source: "Storage Lens", checked_at: checkedAgo(240) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_14", name: "billing-api", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · billing",
    owner_note: "billing-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_15", name: "inventory-api", provider: "kubernetes",
    environment: "production", owner: "Checkout", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · inventory",
    owner_note: "inventory-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_16", name: "auth-session-service", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · identity",
    owner_note: "identity-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "Prometheus", checked_at: checkedAgo(120) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_17", name: "product-catalog-api", provider: "kubernetes",
    environment: "production", owner: "Checkout", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · catalog",
    owner_note: "catalog-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(1) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_18", name: "search-indexer", provider: "kubernetes",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · search",
    owner_note: "search-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(3) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_19", name: "email-dispatch-worker", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · messaging",
    owner_note: "messaging-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_20", name: "invoice-pdf-worker", provider: "kubernetes",
    environment: "staging", owner: "Checkout", region: "us-west-2",
    source: "staging-us-west-2", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · invoices",
    owner_note: "billing-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "unmonitored" },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_21", name: "fraud-scoring-service", provider: "kubernetes",
    environment: "production", owner: "ML Ops", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · fraud",
    owner_note: "fraud-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_22", name: "shipping-rate-api", provider: "kubernetes",
    environment: "production", owner: "", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · shipping",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_23", name: "webhook-delivery-worker", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · webhooks",
    owner_note: "messaging-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "Datadog", checked_at: checkedAgo(180) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_24", name: "production-orders-backup", provider: "aws",
    environment: "production", owner: "Checkout", region: "us-east-1",
    source: "acme-prod-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · orders backup",
    owner_note: "data-infra",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_25", name: "customer-profile-database", provider: "aws",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "acme-prod-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · PostgreSQL profiles",
    owner_note: "identity-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_26", name: "billing-ledger-database", provider: "aws",
    environment: "production", owner: "Checkout", region: "us-east-1",
    source: "acme-prod-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · Aurora ledger",
    owner_note: "billing-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(2) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_27", name: "staging-checkout-database", provider: "aws",
    environment: "staging", owner: "Checkout", region: "us-west-2",
    source: "acme-staging-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · staging PostgreSQL",
    owner_note: "qa-devops",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(3) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_28", name: "reporting-read-replica", provider: "aws",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "acme-prod-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · reporting replica",
    owner_note: "data-infra",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "CloudWatch", checked_at: checkedAgo(240) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_29", name: "event-ingestion-database", provider: "aws",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "acme-prod-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · event ingestion",
    owner_note: "data-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "CloudWatch", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_30", name: "dev-integration-database", provider: "aws",
    environment: "development", owner: "", region: "us-west-2",
    source: "acme-dev-aws", kind: "database",
  }, "aws.rds.instance", "model_only", {
    description: "Amazon RDS · integration testing",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "unmonitored" },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_31", name: "product-images-bucket", provider: "aws",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "acme-prod-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · product images",
    owner_note: "media-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(5) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_32", name: "invoice-archive-bucket", provider: "aws",
    environment: "production", owner: "Checkout", region: "us-east-1",
    source: "acme-prod-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · invoice archive",
    owner_note: "billing-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(5) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_33", name: "data-export-bucket", provider: "aws",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "acme-prod-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · data exports",
    owner_note: "data-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(4) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_34", name: "staging-assets-bucket", provider: "aws",
    environment: "staging", owner: "Infra Core", region: "us-west-2",
    source: "acme-staging-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · staging assets",
    owner_note: "qa-devops",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(5) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_35", name: "telemetry-retention-bucket", provider: "aws",
    environment: "production", owner: "Infra Core", region: "us-east-1",
    source: "acme-prod-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · telemetry retention",
    owner_note: "observability-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "Storage Lens", checked_at: checkedAgo(360) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_36", name: "compliance-audit-bucket", provider: "aws",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "acme-prod-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · compliance audit",
    owner_note: "security-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Storage Lens", checked_at: checkedAgo(3) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_37", name: "abandoned-upload-bucket", provider: "aws",
    environment: "development", owner: "", region: "us-west-2",
    source: "acme-dev-aws", kind: "storage",
  }, "storage.bucket", "model_only", {
    description: "Amazon S3 Bucket · temporary uploads",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "unmonitored" },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_38", name: "knowledge-search-assistant", provider: "aws",
    environment: "production", owner: "genai-sre", region: "us-east-1",
    source: "acme-prod-aws", kind: "ai",
  }, "llm.application", "model_only", {
    description: "LLM Application · knowledge search",
    owner_note: "ai-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "OTel Gateway", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_39", name: "incident-summary-agent", provider: "aws",
    environment: "production", owner: "Infra Core", region: "us-east-1",
    source: "acme-prod-aws", kind: "ai",
  }, "llm.application", "model_only", {
    description: "LLM Application · incident summaries",
    owner_note: "observability-sre",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "stale", source: "OTel Gateway", checked_at: checkedAgo(180) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_40", name: "developer-help-agent", provider: "aws",
    environment: "development", owner: "Platform", region: "us-west-2",
    source: "acme-dev-aws", kind: "ai",
  }, "llm.application", "model_only", {
    description: "LLM Application · developer support",
    owner_note: "developer-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "OTel Gateway", checked_at: checkedAgo(2) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_41", name: "document-classification-agent", provider: "aws",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "acme-prod-aws", kind: "ai",
  }, "llm.application", "model_only", {
    description: "LLM Application · document classification",
    owner_note: "data-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "OTel Gateway", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_42", name: "batch-training-pool", provider: "kubernetes",
    environment: "production", owner: "ML Ops", region: "us-east-1",
    source: "prod-us-east-1", kind: "gpu",
  }, "kubernetes.node_group", "unsupported", {
    description: "Kubernetes GPU node group · batch training",
    owner_note: "ml-ops",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_43", name: "nightly-etl-workers", provider: "kubernetes",
    environment: "production", owner: "Data Eng", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.node_group", "unsupported", {
    description: "Kubernetes node group · nightly ETL",
    owner_note: "data-platform",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(2) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_44", name: "image-render-workers", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "gpu",
  }, "kubernetes.node_group", "unsupported", {
    description: "Kubernetes GPU node group · image rendering",
    owner_note: "media-platform",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_45", name: "canary-api-deployment", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · canary release",
    owner_note: "release-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_46", name: "staging-search-api", provider: "kubernetes",
    environment: "staging", owner: "Data Eng", region: "us-west-2",
    source: "staging-us-west-2", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · staging search",
    owner_note: "qa-devops",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(2) },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_47", name: "dev-preview-worker", provider: "kubernetes",
    environment: "development", owner: "", region: "us-west-2",
    source: "dev-us-west-2", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · preview environments",
    optimization_status: "awaiting_evidence", action_path_available: true,
    telemetry: { status: "unmonitored" },
    policy_coverage: "uncovered",
  }),
  mockResource({
    id: "res_48", name: "feature-flag-service", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · feature flags",
    owner_note: "developer-platform",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_49", name: "secrets-rotation-worker", provider: "kubernetes",
    environment: "production", owner: "Platform", region: "us-east-1",
    source: "prod-us-east-1", kind: "compute",
  }, "kubernetes.deployment", "fully_supported", {
    description: "Kubernetes Deployment · secret rotation",
    owner_note: "security-sre",
    optimization_status: "no_action_needed", action_path_available: true,
    telemetry: { status: "healthy", source: "Datadog", checked_at: checkedAgo(1) },
    policy_coverage: "covered",
  }),
  mockResource({
    id: "res_50", name: "legacy-import-worker", provider: "kubernetes",
    environment: "staging", owner: "", region: "us-west-2",
    source: "staging-us-west-2", kind: "compute",
  }, "kubernetes.node_group", "unsupported", {
    description: "Kubernetes node group · legacy imports",
    optimization_status: "action_unsupported", action_path_available: false,
    telemetry: { status: "healthy", source: "Prometheus", checked_at: checkedAgo(4) },
    policy_coverage: "uncovered",
  }),
];
