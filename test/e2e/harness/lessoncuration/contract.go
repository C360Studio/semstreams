// Package lessoncuration defines the E2E-only operator control contract used
// to exercise lesson promotion through the already-bound framework client.
package lessoncuration

// SubjectPromote is the E2E-only request subject for lesson promotion.
const SubjectPromote = "e2e.control.lesson.promote"

// PromoteRequest identifies the lesson entity the E2E app should promote.
type PromoteRequest struct {
	EntityID string `json:"entity_id"`
}

// PromoteResponse confirms that the requested lesson promotion completed.
type PromoteResponse struct {
	Promoted bool `json:"promoted"`
}
