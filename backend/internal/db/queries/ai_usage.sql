-- @spec AI-030, AI-034
-- name: RecordAIUsage :exec
INSERT INTO ai_usage (user_id, usage_date, feature, calls, input_tokens, output_tokens, cost_micros)
VALUES (sqlc.arg(user_id), CURRENT_DATE, sqlc.arg(feature), 1, sqlc.arg(input_tokens), sqlc.arg(output_tokens), sqlc.arg(cost_micros))
ON CONFLICT (user_id, usage_date, feature) DO UPDATE SET
    calls         = ai_usage.calls + 1,
    input_tokens  = ai_usage.input_tokens + EXCLUDED.input_tokens,
    output_tokens = ai_usage.output_tokens + EXCLUDED.output_tokens,
    cost_micros   = ai_usage.cost_micros + EXCLUDED.cost_micros;

-- Calls this user has already made of one feature today. Wrapped in COALESCE so
-- a user with no row yet returns 0 rather than no rows.
-- @spec AI-031
-- name: CountAIFeatureCallsToday :one
SELECT COALESCE(
    (SELECT calls FROM ai_usage
     WHERE user_id = sqlc.arg(user_id) AND usage_date = CURRENT_DATE AND feature = sqlc.arg(feature)),
    0)::int;

-- Month-to-date estimated spend across every user, in micro-USD.
-- @spec AI-032
-- name: SumAICostMicrosSince :one
SELECT COALESCE(SUM(cost_micros), 0)::bigint
FROM ai_usage
WHERE usage_date >= sqlc.arg(since);
