package agentic

import (
	"context"
	"fmt"
	"os/exec"
)

type commandRunner func(context.Context, string, ...string) error

type composeProcessController struct {
	composeFile string
	service     string
	run         commandRunner
}

func newComposeProcessController(composeFile string) composeProcessController {
	return composeProcessController{
		composeFile: composeFile,
		service:     "semstreams",
		run: func(ctx context.Context, name string, args ...string) error {
			command := exec.CommandContext(ctx, name, args...)
			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s %v: %w: %s", name, args, err, output)
			}
			return nil
		},
	}
}

func (c composeProcessController) validate() error {
	if c.composeFile == "" {
		return fmt.Errorf("agentic compose file is empty")
	}
	if c.service == "" {
		return fmt.Errorf("agentic compose service is empty")
	}
	if c.run == nil {
		return fmt.Errorf("agentic compose command runner is nil")
	}
	return nil
}

func (c composeProcessController) kill(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	if err := c.run(ctx, "docker", "compose", "-f", c.composeFile, "kill", "-s", "SIGKILL", c.service); err != nil {
		return fmt.Errorf("kill SemStreams process: %w", err)
	}
	return nil
}

func (c composeProcessController) start(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	if err := c.run(ctx, "docker", "compose", "-f", c.composeFile, "up", "-d", "--wait", "--no-deps", c.service); err != nil {
		return fmt.Errorf("start replacement SemStreams process: %w", err)
	}
	return nil
}
