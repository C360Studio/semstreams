package rule

import "strings"

const reservedUserResponseSubjectFamily = "user.response.>"

// targetsReservedUserResponseSubject reports whether subject is inside the
// token family reserved for typed agentic.user_response.v1 messages. It is
// deliberately private: this is a rule-writer guard, not a configurable
// namespace registry or adopter override surface.
func targetsReservedUserResponseSubject(subject string) bool {
	tokens := strings.Split(subject, ".")
	return len(tokens) >= 3 && tokens[0] == "user" && tokens[1] == "response"
}

func isArbitrarySubjectPublisher(actionType string) bool {
	switch actionType {
	case ActionTypePublish, ActionTypePublishAgent, ActionTypeApprove:
		return true
	default:
		return false
	}
}
