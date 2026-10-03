# Universal Resource Model

The resource registry gives every infrastructure object one common shape. A Kubernetes Deployment, database instance, storage bucket, or application can therefore move through the same evidence, recommendation, policy, action, verification, and audit lifecycle without putting provider-specific fields into the core platform.

## Identity

New resources receive an ID derived from five immutable coordinates:

```text
provider + account + location + resource type + provider resource ID
```

For example:

```text
resource:v1:kubernetes:docker-desktop:local:kubernetes.deployment:consize-demo%2Fcheckout-api
```

`account` means the provider boundary that prevents collisions. It is an AWS account, GCP project, Azure subscription, Kubernetes cluster, or the equivalent boundary for another provider. `location` is a region, zone, cluster-local scope, or another explicit location value.

Names, labels, ownership, environment, cost, health, utilization, and resource sizing are mutable. Changing them does not change identity.

## Core fields

Every registered resource must provide:

- immutable provider identity;
- a human-readable name, owner, environment, and criticality;
- lifecycle and implementation-support status;
- provider metadata and current state;
- observation, first-seen, last-seen, creation, and update timestamps;
- the plugin that supplied the latest observation, when applicable.

Provider-specific values belong in `metadata` or `current_state`. They cannot replace or override core identity and lifecycle fields.

## Support status

Support is derived by Consize and cannot be claimed by a plugin:

- `fully_supported`: the current release can discover and operate the type through its governed path;
- `model_only`: the type has a stable common representation, but the release does not yet provide complete discovery and action behavior;
- `unsupported`: the registry can represent the object without suggesting that Consize can optimize it.

In the current alpha, Kubernetes Deployments are fully supported. AWS RDS instances, storage buckets, and LLM applications are model-only. Other types are explicitly unsupported until an approved capability is added.

## Lifecycle

Resources begin as `active`. A provider may mark an object `stale` when it is no longer observed and `deleted` when deletion is confirmed. A deleted resource may return to active if the same provider identity reappears. It cannot move directly from deleted to stale.

The registry rejects observations older than the latest accepted observation. This prevents delayed discovery runs from replacing current state.

## Collision handling

The registry rejects:

- one ID being reused for another immutable identity;
- one immutable identity being registered under multiple current IDs;
- duplicate identities in durable state;
- incomplete provider coordinates and control characters in identity fields.

During the v0.3 alpha migration, a pre-existing noncanonical ID is retained when the same immutable resource is rediscovered with a canonical ID. This preserves recommendation, action, and audit references. New resources always use canonical IDs.

## Provider extensions

`metadata` stores descriptive provider facts such as Kubernetes namespace, UID, generation, or AWS ARN attributes. `current_state` stores observed operating configuration such as CPU and memory requests. Both fields round-trip as JSON objects, but neither participates in identity.

Plugins must not place credentials, access tokens, private keys, or unredacted secret values in either field.

## Durable migration

State schema v3 adds the resource schema version, lifecycle, support status, and first/last-seen timestamps. Migration populates these fields from existing timestamps but never guesses missing provider identity. If an older record lacks provider, account, location, type, or provider resource ID, startup fails with the affected resource ID so an operator can repair or deliberately recreate the local alpha state.

This fail-closed behavior prevents two real resources from being merged under invented identity.

If startup reports an invalid durable resource, follow [Local State and
Recovery](local-state.md). Back up the state before doing anything else; reset
is only appropriate for disposable alpha development data.
