-- The feature flags: one row per flag the resources package declares (a resource.Feature
-- constant), written by the deploy's resource.MigrateFeatures and flipped by SetFeature;
-- FeatureFlagChanges records every flip in the same commit. The statements are the
-- library's own (resource.FeatureFlagsDDL(resource.SpannerDBType)), which impulse check
-- (feature-flags) compares them with.

CREATE TABLE FeatureFlags (
  Name STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Enabled BOOL NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  UpdatedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name);

CREATE TABLE FeatureFlagChanges (
  Name STRING(64) NOT NULL,
  ChangedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  Enabled BOOL NOT NULL,
  ChangedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name, ChangedAt);
