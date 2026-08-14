package rule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateActionListsRejectsReservedUserResponseSubjects(t *testing.T) {
	actionTypes := []string{ActionTypePublish, ActionTypePublishAgent, ActionTypeApprove}
	subjects := []string{"user.response.cli.channel-1", "user.response.$entity.instance"}
	lists := []struct {
		name string
		set  func(*Definition, Action)
	}{
		{name: "on_enter", set: func(def *Definition, action Action) { def.OnEnter = []Action{action} }},
		{name: "on_exit", set: func(def *Definition, action Action) { def.OnExit = []Action{action} }},
		{name: "while_true", set: func(def *Definition, action Action) { def.WhileTrue = []Action{action} }},
		{name: "on_recovery", set: func(def *Definition, action Action) { def.OnRecovery = []Action{action} }},
		{name: "actions", set: func(def *Definition, action Action) { def.Actions = []Action{action} }},
	}

	for _, actionType := range actionTypes {
		for _, subject := range subjects {
			for _, list := range lists {
				t.Run(actionType+"/"+list.name+"/"+subject, func(t *testing.T) {
					def := Definition{ID: "reserved-subject-rule"}
					list.set(&def, Action{Type: actionType, Subject: subject})

					err := validateActionLists(def)
					require.Error(t, err)
					require.ErrorContains(t, err, "reserved-subject-rule")
					require.ErrorContains(t, err, list.name+"[0]")
					require.ErrorContains(t, err, actionType)
					require.ErrorContains(t, err, "user.response.>")
				})
			}
		}
	}
}

func TestValidateActionListsAllowsUnrelatedUserResponsesPrefix(t *testing.T) {
	for _, actionType := range []string{ActionTypePublish, ActionTypePublishAgent, ActionTypeApprove} {
		t.Run(actionType, func(t *testing.T) {
			def := Definition{
				ID:      "unrelated-subject-rule",
				OnEnter: []Action{{Type: actionType, Subject: "user.responses.audit"}},
			}
			require.NoError(t, validateActionLists(def))
		})
	}
}

func TestActionExecutorRejectsDynamicReservedUserResponseSubjectBeforeSideEffects(t *testing.T) {
	tests := []struct {
		name   string
		action Action
	}{
		{
			name:   ActionTypePublish,
			action: Action{Type: ActionTypePublish, Subject: "$message.target"},
		},
		{
			name: ActionTypePublishAgent,
			action: Action{
				Type: ActionTypePublishAgent, Subject: "$message.target",
				Role: "worker", Model: "mock", Prompt: "do work",
			},
		},
		{
			name:   ActionTypeApprove,
			action: Action{Type: ActionTypeApprove, Subject: "$message.target", Reason: "approved"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &mockPublisher{}
			auditor := &mockVerdictAuditor{}
			executor := NewActionExecutorFull(nil, nil, publisher)
			executor.SetVerdictAuditor(auditor)
			ec := &ExecutionContext{
				EntityID:    "c360.platform.test.system.entity.001",
				MessageData: map[string]any{"target": "user.response.cli.channel-1"},
			}

			err := executor.Execute(context.Background(), tt.action, ec)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.name)
			require.ErrorContains(t, err, "user.response.cli.channel-1")
			require.Empty(t, publisher.published, "reserved-subject rejection must precede publication")
			require.Empty(t, auditor.emitted, "reserved-subject rejection must precede approve audit")
		})
	}
}
