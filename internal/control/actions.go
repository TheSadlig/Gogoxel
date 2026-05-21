package control

import "Gogoxel/internal/input"

const (
	ActionMoveForward  input.Action = "move_forward"
	ActionMoveBackward input.Action = "move_backward"
	ActionMoveLeft     input.Action = "move_left"
	ActionMoveRight    input.Action = "move_right"
	ActionMoveAway     input.Action = "move_away"
	ActionMoveCloser   input.Action = "move_closer"
	ActionMoveUp       input.Action = "move_up"
	ActionMoveDown     input.Action = "move_down"
	ActionYawDown      input.Action = "yaw_down"
	ActionYawUp        input.Action = "yaw_up"
	ActionTurnRight    input.Action = "turn_right"
	ActionTurnLeft     input.Action = "turn_left"
	ActionFaster       input.Action = "faster"
	ActionNextModel    input.Action = "next_model"
	ActionPlaceCube    input.Action = "place_cube"
	ActionRemoveCube   input.Action = "remove_cube"
	ActionNextMaterial input.Action = "next_material"
	ActionPreviousMaterial input.Action = "previous_material"
)

func KnownActions() []input.Action {
	return []input.Action{
		ActionMoveForward,
		ActionMoveBackward,
		ActionMoveLeft,
		ActionMoveRight,
		ActionMoveAway,
		ActionMoveCloser,
		ActionMoveUp,
		ActionMoveDown,
		ActionYawDown,
		ActionYawUp,
		ActionTurnRight,
		ActionTurnLeft,
		ActionFaster,
		ActionNextModel,
		ActionPlaceCube,
		ActionRemoveCube,
		ActionNextMaterial,
		ActionPreviousMaterial,
	}
}