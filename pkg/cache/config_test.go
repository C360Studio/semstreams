package cache

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConfig_UnmarshalJSON_DurationStrings(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		want     Config
		wantErr  bool
	}{
		{
			name: "duration strings",
			jsonData: `{
				"enabled": true,
				"strategy": "hybrid",
				"max_size": 1000,
				"ttl": "1h",
				"cleanup_interval": "5m",
				"stats_interval": "30s"
			}`,
			want: Config{
				Enabled:         true,
				Strategy:        StrategyHybrid,
				MaxSize:         1000,
				TTL:             1 * time.Hour,
				CleanupInterval: 5 * time.Minute,
				StatsInterval:   30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "integer nanoseconds (backward compatibility)",
			jsonData: `{
				"enabled": true,
				"strategy": "ttl",
				"ttl": 3600000000000,
				"cleanup_interval": 300000000000
			}`,
			want: Config{
				Enabled:         true,
				Strategy:        StrategyTTL,
				TTL:             1 * time.Hour,
				CleanupInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "mixed formats",
			jsonData: `{
				"enabled": true,
				"strategy": "hybrid",
				"max_size": 500,
				"ttl": "2h30m",
				"cleanup_interval": 60000000000,
				"stats_interval": "1m"
			}`,
			want: Config{
				Enabled:         true,
				Strategy:        StrategyHybrid,
				MaxSize:         500,
				TTL:             2*time.Hour + 30*time.Minute,
				CleanupInterval: 1 * time.Minute,
				StatsInterval:   1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "invalid duration string",
			jsonData: `{
				"enabled": true,
				"ttl": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "minimal config",
			jsonData: `{
				"enabled": false
			}`,
			want: Config{
				Enabled: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Config
			err := json.Unmarshal([]byte(tt.jsonData), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("Config.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if got.Enabled != tt.want.Enabled {
					t.Errorf("Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
				}
				if got.Strategy != tt.want.Strategy {
					t.Errorf("Strategy = %v, want %v", got.Strategy, tt.want.Strategy)
				}
				if got.MaxSize != tt.want.MaxSize {
					t.Errorf("MaxSize = %v, want %v", got.MaxSize, tt.want.MaxSize)
				}
				if got.TTL != tt.want.TTL {
					t.Errorf("TTL = %v, want %v", got.TTL, tt.want.TTL)
				}
				if got.CleanupInterval != tt.want.CleanupInterval {
					t.Errorf("CleanupInterval = %v, want %v", got.CleanupInterval, tt.want.CleanupInterval)
				}
				if got.StatsInterval != tt.want.StatsInterval {
					t.Errorf("StatsInterval = %v, want %v", got.StatsInterval, tt.want.StatsInterval)
				}
			}
		})
	}
}

func TestConfig_UnmarshalJSON_RealWorldExample(t *testing.T) {
	// Test with a real-world objectstore config
	jsonData := `{
		"enabled": true,
		"strategy": "hybrid",
		"max_size": 5000,
		"ttl": "1h",
		"cleanup_interval": "5m"
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
		t.Fatalf("UnmarshalJSON() failed: %v", err)
	}

	if cfg.TTL != 1*time.Hour {
		t.Errorf("TTL = %v, want 1h", cfg.TTL)
	}

	if cfg.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval = %v, want 5m", cfg.CleanupInterval)
	}

	// Verify it validates correctly
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}
}
