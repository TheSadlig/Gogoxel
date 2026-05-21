package control

import (
	"time"

	"Gogoxel/internal/input"
)

const (
	MoveCooldown       = 120 * time.Millisecond
	ModelSwitchCooldown = 300 * time.Millisecond
	EditCooldown       = 0
	MaterialSwitchCooldown = 150 * time.Millisecond
)

type ActionPolicy struct {
	Cooldown time.Duration
}

func DefaultPolicies() map[input.Action]ActionPolicy {
	return map[input.Action]ActionPolicy{
		ActionMoveForward:  {Cooldown: MoveCooldown},
		ActionMoveBackward: {Cooldown: MoveCooldown},
		ActionMoveLeft:     {Cooldown: MoveCooldown},
		ActionMoveRight:    {Cooldown: MoveCooldown},
		ActionMoveAway:     {Cooldown: MoveCooldown},
		ActionMoveCloser:   {Cooldown: MoveCooldown},
		ActionMoveUp:       {Cooldown: MoveCooldown},
		ActionMoveDown:     {Cooldown: MoveCooldown},
		ActionYawDown:      {Cooldown: MoveCooldown},
		ActionYawUp:        {Cooldown: MoveCooldown},
		ActionTurnRight:    {Cooldown: MoveCooldown},
		ActionTurnLeft:     {Cooldown: MoveCooldown},
		ActionFaster:       {Cooldown: MoveCooldown},
		ActionNextModel:    {Cooldown: ModelSwitchCooldown},
		ActionPlaceCube:    {Cooldown: EditCooldown},
		ActionRemoveCube:   {Cooldown: EditCooldown},
		ActionNextMaterial: {Cooldown: MaterialSwitchCooldown},
		ActionPreviousMaterial: {Cooldown: MaterialSwitchCooldown},
	}
}