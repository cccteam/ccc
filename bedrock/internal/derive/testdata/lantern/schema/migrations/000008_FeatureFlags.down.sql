-- Drops the feature flags and their change records; the flags are declared by the
-- release and written again by the next deploy's resource.MigrateFeatures.
DROP TABLE FeatureFlagChanges;

DROP TABLE FeatureFlags;
