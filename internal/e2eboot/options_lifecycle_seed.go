package e2eboot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

const lifecycleSeedVariable = "SEMSTREAMS_E2E_LIFECYCLE_SEED"

// enableLifecycleSeed seeds one mission Participant after every service has
// started. The value is the entity's last four canonical positions. Seeding
// needs the mission workflow (SEMSTREAMS_E2E_MISSION); without it
// Manager.Create fails boot loudly, so no separate check is made here.
func enableLifecycleSeed(opts *boot.Options, suffix string) {
	opts.PostStart = append(opts.PostStart,
		func(ctx context.Context, mgr *lifecycle.Manager, platform types.PlatformMeta) error {
			return seedMission(ctx, mgr, platform, suffix)
		})
}

// seedMission Creates a mission Participant in the planning phase under THIS
// deployment's authority, so the lifecycle gateway has a known instance to
// serve before the scenario runs. Already-exists is treated as a no-op so the
// binary is idempotent across restarts in the e2e fixture.
//
// The value carries the last FOUR canonical positions —
// system.domain.type.instance — and this function composes positions 1-2 from
// the platform. Since ADR-104 the effective platform.id carries an entropy
// suffix minted at first boot, so a compose file cannot spell the pair at all;
// composing here means nothing predicts a value the binary already holds. The
// mission-command processor stamps positions 1-2 from deps.Platform and never
// from the wire, so an entity seeded under any other pair is one no command
// could ever reach.
func seedMission(ctx context.Context, mgr *lifecycle.Manager, platform types.PlatformMeta, seedSuffix string) error {
	if strings.Count(seedSuffix, ".") != 3 {
		return fmt.Errorf(
			"%s %q must be the last four canonical positions "+
				"(system.domain.type.instance); this deployment composes the authority %s.%s itself",
			lifecycleSeedVariable, seedSuffix, platform.Org, platform.Platform)
	}
	entityID := platform.Org + "." + platform.Platform + "." + seedSuffix
	if _, err := semtypes.ParseEntityID(entityID); err != nil {
		return fmt.Errorf("%s %q composes %q, which is not a canonical entity ID: %w",
			lifecycleSeedVariable, seedSuffix, entityID, err)
	}
	state := &mission.State{
		EntityIDField: entityID,
		PhaseField:    mission.PhasePlanning,
	}
	err := mgr.Create(ctx, state)
	if err == nil {
		slog.Info("seeded mission", "entity_id", entityID, "phase", mission.PhasePlanning)
		return nil
	}
	// Manager.Create returns ErrAlreadyExists when the entity is
	// already lifecycle-managed (has the phase triple). Treat as a
	// no-op so the e2e binary is idempotent across restarts.
	if errors.Is(err, lifecycle.ErrAlreadyExists) {
		slog.Info("mission already seeded", "entity_id", entityID)
		return nil
	}
	return err
}
