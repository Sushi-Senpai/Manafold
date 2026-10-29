-- The deck wizard is a metered AI feature of its own (AI-036): one call per
-- round, counted against its own per-user daily limit and the shared monthly
-- ceiling. ai_usage.feature was checked against the two original features, so
-- the check is widened to admit 'wizard'.
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check;
ALTER TABLE ai_usage ADD CONSTRAINT ai_usage_feature_check
    CHECK (feature IN ('suggest', 'explain', 'wizard'));
