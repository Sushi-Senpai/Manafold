-- Per-user, per-day, per-feature meter for the AI features (M4). One row is
-- upserted after each successful language-model call: calls +1 and the token /
-- cost totals accumulated. The per-user daily call quota reads `calls` for
-- (user, today, feature); the global monthly spend ceiling sums `cost_micros`
-- across all users for the current month. cost_micros is millionths of a USD,
-- an integer so the running sum never drifts. See docs/intent/ai-assist/.
CREATE TABLE ai_usage (
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    usage_date    DATE NOT NULL,
    feature       TEXT NOT NULL CHECK (feature IN ('suggest', 'explain')),
    calls         INTEGER NOT NULL DEFAULT 0,
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_micros   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, usage_date, feature)
);

-- The ceiling query filters by usage_date >= the first of the month across
-- every user, so it wants a date index independent of the primary key's
-- user-first ordering.
CREATE INDEX idx_ai_usage_date ON ai_usage (usage_date);
