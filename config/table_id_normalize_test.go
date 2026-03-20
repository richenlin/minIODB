package config

import "testing"

func TestNormalizeAutoGenerateIDFromStrategy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		strategy string
		want     bool
	}{
		{"user_provided", false},
		{"USER_PROVIDED", false},
		{" snowflake ", true},
		{"uuid", true},
		{"custom", true},
		{"", true},
		{"unknown_future", true},
	}
	for _, tc := range cases {
		cfg := &TableConfig{IDStrategy: tc.strategy, AutoGenerateID: !tc.want}
		NormalizeAutoGenerateIDFromStrategy(cfg)
		if cfg.AutoGenerateID != tc.want {
			t.Errorf("strategy=%q: got AutoGenerateID=%v want %v", tc.strategy, cfg.AutoGenerateID, tc.want)
		}
	}
}
