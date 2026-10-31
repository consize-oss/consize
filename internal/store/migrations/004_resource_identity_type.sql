-- Resource type is part of universal identity. Different resource kinds may
-- legitimately share the same provider-local identifier.
ALTER TABLE resources DROP CONSTRAINT resources_provider_account_id_region_provider_resource_id_key;
CREATE UNIQUE INDEX resources_identity_idx
    ON resources(provider,account_id,region,(payload->>'type'),provider_resource_id);
