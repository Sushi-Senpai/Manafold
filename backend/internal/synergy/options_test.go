package synergy

import (
	"testing"
	"time"
)

// @spec SYN-001
func TestOptionsFromEnv_OffByDefaultAndReadsTheLimits(t *testing.T) {
	for _, k := range []string{"SYNERGY_ENABLED", "SYNERGY_DECKS_PER_COMMANDER", "SYNERGY_TTL_DAYS", "SYNERGY_MAX_NEW_COMMANDERS_PER_DAY"} {
		t.Setenv(k, "")
	}
	if o := OptionsFromEnv().withDefaults(); o.Enabled {
		t.Error("synergy is enabled with SYNERGY_ENABLED unset")
	}
	o := OptionsFromEnv().withDefaults()
	if o.DecksPerCommander != 40 || o.TTL != 14*24*time.Hour || o.MaxNewPerDay != 10 || o.FailureCooldown != 15*time.Minute || o.SearchPages != 3 || o.BaselineCommanders != 5 || o.DecksPerBaselineCommander != 8 {
		t.Errorf("defaults = %+v", o)
	}

	t.Setenv("SYNERGY_ENABLED", "true")
	t.Setenv("SYNERGY_DECKS_PER_COMMANDER", "25")
	t.Setenv("SYNERGY_TTL_DAYS", "3")
	t.Setenv("SYNERGY_MAX_NEW_COMMANDERS_PER_DAY", "bogus")
	o = OptionsFromEnv().withDefaults()
	if !o.Enabled || o.DecksPerCommander != 25 || o.TTL != 3*24*time.Hour || o.MaxNewPerDay != 10 {
		t.Errorf("env = %+v, want enabled, 25 decks, 3-day TTL, invalid cap back to 10", o)
	}
}
