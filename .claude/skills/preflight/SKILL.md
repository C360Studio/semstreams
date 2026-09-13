---
name: preflight
description: Select and run the existing SemStreams verification gates for a concrete diff before an implementation push or local-readiness assessment.
argument-hint: "[optional explicit comparison base]"
---

# SemStreams preflight

Read [the canonical semstreams-preflight skill](../../../.agents/skills/semstreams-preflight/SKILL.md) fully and
follow it. Apply any explicit comparison base in `$ARGUMENTS`; otherwise establish the actual PR target as the
canonical skill directs. This adapter preserves `/preflight` and adds no separate gate or authority.
