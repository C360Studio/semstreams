// Package operatingmodel defines the data model and payload types for a user's
// work operating model: the rhythms, decisions, dependencies, institutional
// knowledge, and friction that describe how a user works.
//
// The operating model is populated by the /onboard dispatch command through a
// five-layer elicitation interview and consumed by the agentic loop as
// injected system-prompt context (via teams-memory's hydrator).
//
// # Layers
//
// The interview captures five ordered layers:
//
//  1. operating_rhythms         — recurring cadence and calendar structure
//  2. recurring_decisions       — decisions made on a regular schedule
//  3. dependencies              — people, systems, and inputs the work relies on
//  4. institutional_knowledge   — tribal knowledge and hard-won context
//  5. friction                  — where the work gets stuck today
//
// # Entity model
//
// Operating-model facts land in the knowledge graph as predicate-per-field
// triples under a user-profile subtree:
//
//	{org}.{platform}.user.teams.profile.{userID}                   — profile root
//	{org}.{platform}.user.teams.om-layer.{userID}-{layer}          — per layer
//	{org}.{platform}.user.teams.om-entry.{entryID}                 — per entry
//
// Predicates use the om.* namespace (om.entry.title, om.layer.checkpoint_summary,
// user.operating_model.version, etc.) so operating-model facts can be queried
// cleanly without blurring into other teams-memory categories such as
// lessons-learned.
//
// # Message types
//
// Two payload types are registered with the global component.PayloadRegistry
// in init():
//
//   - operating_model.layer_approved.v1   — emitted when a layer checkpoint is
//     approved by the user; consumed by teams-memory to write triples.
//   - operating_model.profile_context.v1  — published by the teams-memory
//     hydrator on loop-starting events and consumed by teams-loop to inject a
//     "How this user works" section into the system prompt.
package operatingmodel
