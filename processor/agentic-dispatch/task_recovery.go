package agenticdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const dispatchTaskIDPrefix = "dispatch-"

type preparedDispatchTask struct {
	task    agentic.TaskMessage
	data    []byte
	subject string
}

type vacantDispatchTaskSlot struct {
	taskID  string
	subject string
}

type retainedTaskEvidenceReader interface {
	ReadRetainedTask(context.Context, string, string) ([]byte, bool, error)
}

type natsRetainedTaskEvidenceReader struct {
	client *natsclient.Client
}

func (r natsRetainedTaskEvidenceReader) ReadRetainedTask(
	ctx context.Context,
	streamName string,
	subject string,
) ([]byte, bool, error) {
	stream, err := r.client.GetStream(ctx, streamName)
	if err != nil {
		return nil, false, fmt.Errorf("read task stream %s: %w", streamName, err)
	}
	raw, err := stream.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read retained task %s: %w", subject, err)
	}
	return append([]byte(nil), raw.Data...), true, nil
}

// stableDispatchTaskID derives the dispatch task identity only from the
// validated identity fields of its durable UserMessage source. Length-prefixing
// keeps the tuple unambiguous without asking callers to construct an ID.
func stableDispatchTaskID(msg agentic.UserMessage) string {
	hash := sha256.New()
	for _, part := range []string{msg.MessageID, msg.ChannelType, msg.ChannelID, msg.UserID} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}
	return dispatchTaskIDPrefix + hex.EncodeToString(hash.Sum(nil))
}

func (c *Component) findRetainedDispatchTask(
	ctx context.Context,
	msg agentic.UserMessage,
) (preparedDispatchTask, vacantDispatchTaskSlot, bool, error) {
	if err := msg.Validate(); err != nil {
		return preparedDispatchTask{}, vacantDispatchTaskSlot{}, false, err
	}
	taskID := stableDispatchTaskID(msg)
	subject, streamName, err := dispatchTaskAddress(c.outputPortDefs(), taskID)
	if err != nil {
		return preparedDispatchTask{}, vacantDispatchTaskSlot{}, false, err
	}
	slot := vacantDispatchTaskSlot{taskID: taskID, subject: subject}

	retained, retainedData, found, err := c.readRetainedDispatchTask(ctx, streamName, subject)
	if err != nil {
		return preparedDispatchTask{}, vacantDispatchTaskSlot{}, false, err
	}
	if found {
		if err := validateRetainedDispatchTask(retained, msg, taskID, msg.ReplyTo); err != nil {
			return preparedDispatchTask{}, vacantDispatchTaskSlot{}, false, errs.WrapFatal(
				err, "Component", "findRetainedDispatchTask", "task mapping conflict")
		}
		return preparedDispatchTask{task: retained, data: retainedData, subject: subject}, slot, true, nil
	}
	return preparedDispatchTask{}, slot, false, nil
}

func (c *Component) prepareNewDispatchTask(
	ctx context.Context,
	msg agentic.UserMessage,
	loopID string,
	slot vacantDispatchTaskSlot,
) (preparedDispatchTask, error) {
	if loopID == "" {
		loopID = uuid.NewString()
	}
	task := c.buildTaskMessage(ctx, msg, loopID, slot.taskID)
	data, err := json.Marshal(message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch"))
	if err != nil {
		return preparedDispatchTask{}, err
	}
	return preparedDispatchTask{task: task, data: data, subject: slot.subject}, nil
}

func dispatchTaskAddress(ports []component.PortDefinition, taskID string) (string, string, error) {
	subject, err := component.ResolveSubject(ports, "agent.task", taskID)
	if err != nil {
		return "", "", err
	}

	for _, definition := range ports {
		if definition.Name != "agent.task" {
			continue
		}
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			return "", "", err
		}
		facts, err := port.Facts()
		if err != nil {
			return "", "", err
		}
		stream, ok := facts.Stream()
		if !ok || stream.Name() == "" {
			return "", "", fmt.Errorf("agent.task output does not declare a JetStream stream")
		}
		return subject, stream.Name(), nil
	}
	return "", "", fmt.Errorf("agent.task output not found")
}

func (c *Component) readRetainedDispatchTask(
	ctx context.Context,
	streamName string,
	subject string,
) (agentic.TaskMessage, []byte, bool, error) {
	reader := c.taskEvidence
	if reader == nil {
		reader = natsRetainedTaskEvidenceReader{client: c.natsClient}
	}
	raw, found, err := reader.ReadRetainedTask(ctx, streamName, subject)
	if err != nil {
		return agentic.TaskMessage{}, nil, false, errs.WrapTransient(
			err, "Component", "readRetainedDispatchTask", "read retained task evidence")
	}
	if !found {
		return agentic.TaskMessage{}, nil, false, nil
	}

	decoded, err := c.decoder.Decode(raw)
	if err != nil {
		return agentic.TaskMessage{}, nil, false, errs.WrapFatal(
			err, "Component", "readRetainedDispatchTask", "task mapping conflict: decode retained task")
	}
	if err := decoded.Validate(); err != nil {
		return agentic.TaskMessage{}, nil, false, errs.WrapFatal(
			err, "Component", "readRetainedDispatchTask", "task mapping conflict: invalid retained task")
	}
	task, ok := decoded.Payload().(*agentic.TaskMessage)
	if !ok {
		return agentic.TaskMessage{}, nil, false, errs.WrapFatal(
			fmt.Errorf("unexpected retained payload type %T", decoded.Payload()),
			"Component", "readRetainedDispatchTask", "task mapping conflict")
	}
	return *task, append([]byte(nil), raw...), true, nil
}

func validateRetainedDispatchTask(
	task agentic.TaskMessage,
	msg agentic.UserMessage,
	taskID string,
	requestedLoopID string,
) error {
	switch {
	case task.TaskID != taskID:
		return fmt.Errorf("retained task_id %q does not match %q", task.TaskID, taskID)
	case task.LoopID == "":
		return fmt.Errorf("retained task has no loop_id")
	case requestedLoopID != "" && task.LoopID != requestedLoopID:
		return fmt.Errorf("retained loop_id %q does not match requested loop_id %q", task.LoopID, requestedLoopID)
	case task.SourceMessageID != msg.MessageID:
		return fmt.Errorf("retained source_message_id %q does not match %q", task.SourceMessageID, msg.MessageID)
	case task.ChannelType != msg.ChannelType:
		return fmt.Errorf("retained channel_type %q does not match %q", task.ChannelType, msg.ChannelType)
	case task.ChannelID != msg.ChannelID:
		return fmt.Errorf("retained channel_id %q does not match %q", task.ChannelID, msg.ChannelID)
	case task.UserID != msg.UserID:
		return fmt.Errorf("retained user_id %q does not match %q", task.UserID, msg.UserID)
	case task.Prompt != msg.Content:
		return fmt.Errorf("retained prompt does not match source content")
	case task.ContextRequestID != msg.ContextRequestID:
		return fmt.Errorf("retained context_request_id does not match source")
	case task.RunID != msg.RunID:
		return fmt.Errorf("retained run_id does not match source")
	case task.InReplyTo != msg.InReplyTo:
		return fmt.Errorf("retained in_reply_to does not match source")
	default:
		return nil
	}
}
