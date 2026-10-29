-- Wizard rows cannot satisfy the narrower check, so they go with it.
DELETE FROM ai_usage WHERE feature = 'wizard';
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check;
ALTER TABLE ai_usage ADD CONSTRAINT ai_usage_feature_check
    CHECK (feature IN ('suggest', 'explain'));
