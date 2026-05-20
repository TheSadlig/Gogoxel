Feature: Deterministic movement over automation gRPC

  Scenario: Move forward deterministically in headless mode
    Given an automation session is started in headless mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the camera is set to x 0 y 0 z 0 yaw 0 pitch 0 fov 60
    When I hold the action "move_forward"
    And I advance the simulation by 20 ticks
    And I release the action "move_forward"
    And I export the trace artifact "movement-trace"
    Then the camera x position should be approximately 2.0 within 0.05
    And the trace artifact should exist
    And the trace artifact should contain "step_ticks"
    And the trace artifact should contain "export_trace"