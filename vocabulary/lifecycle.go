package vocabulary

const (
	// LifecycleTransitionFrom records the phase an occurrence left.
	LifecycleTransitionFrom = "lifecycle.transition.from"
	// LifecycleTransitionTo records the phase an occurrence entered.
	LifecycleTransitionTo = "lifecycle.transition.to"
	// LifecycleTransitionAt records the occurrence timestamp.
	LifecycleTransitionAt = "lifecycle.transition.at"
	// LifecycleTransitionSource records the typed transition trigger.
	LifecycleTransitionSource = "lifecycle.transition.source"
	// LifecycleTransitionNote records an optional operator-facing annotation.
	LifecycleTransitionNote = "lifecycle.transition.note"
)

func init() {
	for _, predicate := range []string{
		LifecycleTransitionFrom,
		LifecycleTransitionTo,
		LifecycleTransitionAt,
		LifecycleTransitionSource,
		LifecycleTransitionNote,
	} {
		Register(predicate, WithDescription("Lifecycle operator-window transition record"))
	}
}
