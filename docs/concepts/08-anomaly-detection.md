# Anomaly Detection

> **Tier 1+ Feature**: Discovers missing relationships by analyzing graph topology.

Anomaly detection complements explicit triples and rules by detecting potential missing relationships through graph analysis. It runs as a background process after community detection and produces suggestions for LLM and/or human review—or auto-approval when confidence exceeds a configured threshold.

## Overview

SemStreams builds graphs progressively across tiers:

| Source | Tier | How it works | Timing |
|--------|------|--------------|--------|
| **Explicit Triples** | 0+ | Ingested from source data | Sync, per-event |
| **Rules Engine** | 0+ | Pattern matching creates new triples | Sync, per-event |
| **Anomaly Detection** | 1+ | Topology analysis suggests missing edges | Async, after clustering |

Anomaly detection requires embeddings and community detection (Tier 1+) because it analyzes structural patterns within and across communities.

**Key question answered**: "Based on the graph's shape, what relationships might be missing?"

---

## How It Works

### Step 1: Compute Structural Indices

After community detection, SemStreams computes two indices:

**K-Core Index**: Measures each entity's structural importance

```
Graph Layers (Onion Model):
┌─────────────────────────────────────┐
│  Periphery (core-1)                 │  Leaf nodes, sparse connections
│  ┌───────────────────────────────┐  │
│  │  Middle (core-2)              │  │  Moderately connected
│  │  ┌─────────────────────────┐  │  │
│  │  │  Backbone (core-3+)     │  │  │  Dense, well-connected hubs
│  │  └─────────────────────────┘  │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

| Core | Meaning | Example |
|------|---------|---------|
| 1 | Peripheral—at least 1 connection | Leaf sensors, one-off events |
| 2 | Connected—part of a chain | Equipment in a zone |
| 3+ | Backbone—highly interconnected | Control units, key documents |

**Pivot distances**: Enable fast in-memory distance estimation for anomaly detectors

```
         P (pivot)
        /|\
       / | \
   d=2/  |  \d=4
     /   |   \
    A    |    B
         |
    lower = |2-4| = 2
    upper = 2+4 = 6
    
"A and B are between 2 and 6 hops apart"
```

Uses the triangle inequality to bound distances without expensive BFS traversal.

### Step 2: Detect Anomalies

Anomaly detectors analyze the indices to find patterns suggesting missing relationships:

#### Semantic-Structural Gap

**Signal**: High embedding similarity + high graph distance

```
Entity A ←─── 0.85 similarity ───→ Entity B
     │                                  │
     └───────── 5 hops apart ───────────┘
```

**Interpretation**: Similar content but no direct connection—is there a missing relationship?

#### Core Isolation

**Signal**: High k-core entity with unexpectedly few same-core peers

```
Expected (core-3):        Actual (isolated):
       A                        A
      /|\                      /
     B C D                    B
   (all core-3)          (only 1 peer)
```

**Interpretation**: A hub without expected dense connections. Relationships may be missing.

#### Core Demotion

**Signal**: Entity's core number dropped between computations

```
Before: Entity X is core-5 (well-connected hub)
After:  Entity X is core-3 (lost connections)
```

**Interpretation**: Lost structural importance—were relationships deleted or is data incomplete?

#### Transitivity Gap

**Signal**: A→B→C chain exists but A-C distance exceeds expectation for transitive predicates

```
Alice ──member_of──→ Engineering ──part_of──→ Company
  │                                              │
  └─────────── 3 hops (expected: 2) ─────────────┘
```

**Interpretation**: Transitive predicates should create implied paths. A gap suggests missing edges.

### Step 3: Queue for Review

Detected anomalies are stored in the ANOMALY_INDEX KV bucket with:

- Entity IDs involved
- Anomaly type and confidence score
- Evidence (similarity scores, distances, core levels)
- Suggested relationship (if applicable)

### Step 4: Review and Apply

```
┌───────────┐     ┌───────────────┐     ┌──────────────┐
│ Detection │────►│ ANOMALY_INDEX │────►│    Review    │
│           │     │   (pending)   │     │ (LLM/human)  │
└───────────┘     └───────────────┘     └──────┬───────┘
                                               │
                         ┌─────────────────────┼─────────────────────┐
                         │                     │                     │
                         ▼                     ▼                     ▼
                   ┌──────────┐          ┌──────────┐          ┌──────────┐
                   │ Applied  │          │ Rejected │          │  Human   │
                   │ (creates │          │(dismissed)│          │  Review  │
                   │  triple) │          │          │          │ (queued) │
                   └──────────┘          └──────────┘          └──────────┘
```

**Confidence thresholds:**

| Confidence | Action |
|------------|--------|
| > 0.9 | Auto-approve (high certainty) |
| < 0.3 | Auto-reject (likely noise) |
| 0.3 - 0.9 | Queue for human/LLM review |

Approved suggestions become new triples in the graph, enriching it for future queries.

---

## When It Runs

Anomaly detection runs as part of the clustering cycle:

```
Entity changes accumulate
        │
        ▼
Community detection triggers (time or threshold)
        │
        ▼
LPA clustering runs
        │
        ▼
Fresh structural inputs computed in memory (k-core, pivot)
        │
        ▼
Anomaly detectors analyze indices
        │
        ▼
Anomalies stored for review
```

This is background processing—queries continue to run against the current graph state.

---

## Configuration

Anomaly detection is controlled through clustering configuration. Key parameters fall into three categories:

### Structural Prerequisites

`enable_anomaly_detection` is the sole switch for structural anomaly work. When it
is enabled, graph-clustering computes K-core and pivot inputs using internal
defaults and passes them directly to the detectors for that cycle. There is no
separate structural configuration or durable structural index.

### Anomaly Detection Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enable_anomaly_detection` | false | Enable anomaly detection and its structural prerequisites |
| `max_anomalies_per_run` | 100 | Cap anomalies per detection cycle |
| `detection_timeout` | 30s | Maximum time for detection to complete |

### Detector-Specific Settings

**Semantic-Structural Gap Detector**

| Parameter | Default | Effect |
|-----------|---------|--------|
| `min_semantic_similarity` | 0.7 | Minimum embedding similarity to consider |

Higher similarity threshold produces fewer but more confident gap detections.

**Virtual Edge Auto-Apply**

| Parameter | Default | Effect |
|-----------|---------|--------|
| `auto_apply.enabled` | false | Enable automatic edge creation |
| `auto_apply.min_confidence` | 0.95 | Confidence threshold for auto-apply |
| `review_queue.enabled` | false | Queue uncertain gaps for review |
| `review_queue.min_confidence` | 0.7 | Lower bound for review queue |
| `review_queue.max_confidence` | 0.95 | Upper bound (below auto-apply) |

Confidence is a composite score combining similarity, structural distance, and core level factors. Gaps with confidence >= 0.95 are auto-applied; those between 0.7-0.95 go to the review queue.

**Core Anomaly Detector**

| Parameter | Default | Effect |
|-----------|---------|--------|
| `min_core_for_hub_analysis` | 2 | Only analyze entities at this core level or above |

Higher values focus detection on structurally important hub entities.

**Transitivity Detector**

| Parameter | Default | Effect |
|-----------|---------|--------|
| `predicates` | (empty) | List of transitive predicates to check |
| `max_path_length` | 3 | Maximum chain length to analyze |

Specify domain predicates like "member_of", "part_of", or "located_in" that imply transitive relationships.

### Review Settings

| Parameter | Default | Effect |
|-----------|---------|--------|
| `auto_approve_threshold` | 0.9 | Auto-approve above this confidence |
| `auto_reject_threshold` | 0.3 | Auto-reject below this confidence |
| `fallback_to_human` | true | Queue uncertain cases for human review |

Anomalies between the thresholds are queued for LLM or human review.

---

## Use Cases

### Telemetry Networks

A sensor network ingests equipment relationships. Anomaly detection finds:
- Sensors semantically similar (same type) but not connected to common zones
- Equipment that lost connections (core demotion) after maintenance

### Document Graphs

A knowledge base links documents via citations. Anomaly detection finds:
- Papers with similar abstracts but no citation relationship
- Authors who should be connected based on co-authorship patterns

### Enterprise Systems

An IT asset graph tracks dependencies. Anomaly detection surfaces:
- Services with similar configurations but no documented dependency
- Servers that dropped out of their usual connectivity cluster

---

## Comparison with Other Inference

| Mechanism | Timing | Input | Output |
|-----------|--------|-------|--------|
| **Rules** | Sync, per-event | Entity patterns | New triples (deterministic) |
| **Anomaly Detection** | Async, after clustering | Graph topology | Suggestions (probabilistic) |
| **Embeddings** | Async, per-entity | Text content | Virtual edges (Tier 1+) |

Anomaly detection is unique in analyzing the **shape** of the graph rather than its content.

---

## Related Concepts

- [Real-Time Inference](00-real-time-inference.md) — Anomaly detection is a Tier 1+ capability
- [Community Detection](07-community-detection.md) — Inference runs after clustering completes
- [Knowledge Graphs](04-knowledge-graphs.md) — Approved suggestions become new triples
- [Similarity Metrics](06-similarity-metrics.md) — Cosine similarity powers semantic gap detection
