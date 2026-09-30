package search

// DefaultQueries returns the standard search quality test queries.
// These cover natural language searches, known-answer validation, and ranking validation.
func DefaultQueries() []Query {
	return []Query{
		// Natural language tests with ranking validation
		{
			Text:            "What documents mention forklift safety?",
			ExpectedPattern: "forklift",
			Description:     "Natural language document search",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"doc-ops-001"}, // Forklift Operation Manual
			MustExclude:     []string{"sensor-temp"}, // Temperature sensors irrelevant
			// Ranking: doc-ops-001 should be in top 3 (observed at rank 2)
			MustIncludeInTopN: map[int][]string{
				3: {"doc-ops-001"},
			},
		},
		{
			Text:            "Are there safety observations related to temperature?",
			ExpectedPattern: "temperature",
			Description:     "Cross-domain safety query",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"sensor-temp"}, // Temperature sensors
			// Ranking: temperature sensors should appear in top 5
			MustIncludeInTopN: map[int][]string{
				5: {"sensor-temp"},
			},
		},
		{
			Text:            "What maintenance was done on cold storage equipment?",
			ExpectedPattern: "cold",
			Description:     "Maintenance semantic search",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"maint-"}, // Maintenance records
		},
		{
			Text:            "Find all sensors in zone-a",
			ExpectedPattern: "zone-a",
			Description:     "Location-based sensor query",
			MinScore:        0.3,
			MinHits:         1,
		},
		// Known-answer tests with ranking validation
		{
			Text:            "forklift operation inspection equipment maintenance",
			ExpectedPattern: "ops",
			Description:     "Operations query should return operations docs",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"doc-ops"},                       // doc-ops-001 (Forklift Operation Manual)
			MustExclude:     []string{"sensor-humid", "sensor-motion"}, // Humidity/motion sensors irrelevant
			// Ranking: doc-ops should be in top 3 (observed at rank 2)
			MustIncludeInTopN: map[int][]string{
				3: {"doc-ops"},
			},
		},
		{
			Text:            "cold storage temperature monitoring refrigeration",
			ExpectedPattern: "temp",
			Description:     "Temperature query should return temp sensors",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"sensor-temp"},         // sensor-temp-001, sensor-temp-002, etc.
			MustExclude:     []string{"doc-hr", "doc-audit"}, // HR and audit docs irrelevant
			// Ranking: temperature sensors should dominate top 5 (observed at ranks 2,3,4)
			MustIncludeInTopN: map[int][]string{
				5: {"sensor-temp"},
			},
			// Relative ranking: sensor-temp should rank higher than doc-hr
			MustRankHigherThan: map[string][]string{
				"sensor-temp": {"doc-hr", "doc-audit"},
			},
		},
		{
			Text:            "hydraulic fluid maintenance equipment repair",
			ExpectedPattern: "maint",
			Description:     "Maintenance query should return maintenance records",
			MinScore:        0.3,
			MinHits:         1,
			MustInclude:     []string{"maint-"}, // maint-001 (hydraulic maintenance)
			// Ranking: maintenance records should be in top 3
			MustIncludeInTopN: map[int][]string{
				3: {"maint-"},
			},
		},
		// Safety policy search - validates safety documents are discoverable
		// Note: BM25 (statistical tier) may rank observations higher due to term frequency
		// The semantic tier will rank these better with neural embeddings
		{
			Text:            "warehouse safety guidelines emergency evacuation fire",
			ExpectedPattern: "content.safety", // Matches both doc-safety-001 and doc-emergency-001
			Description:     "Safety policy query should return safety documents",
			MinScore:        0.1,
			MinHits:         1,
			// At least one safety document should appear in results. The minted ID is
			// <authority>.document.content.safety.doc-safety-001; the earlier
			// "document.safety" pattern predated the content segment and matched
			// nothing, so this known answer failed on every run (#1426).
			MustInclude: []string{"content.safety"},
		},
	}
}

// StructuralQueries returns queries appropriate for structural tier (no embeddings).
// These should all return zero results since structural tier has no semantic search.
func StructuralQueries() []Query {
	return []Query{
		{
			Text:        "forklift safety",
			Description: "Structural tier should return no semantic results",
			MinScore:    0,
			MinHits:     0, // Expect zero hits
		},
	}
}
