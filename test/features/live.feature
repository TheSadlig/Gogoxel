Feature: Live automation over a running session

  Scenario: Move forward in a live headless session
    Given a live automation session is started in headless mode
    And the generator "Cube" is loaded
    And the camera is set to x 0 y 0 z 0 yaw 0 pitch 0 fov 60
    When I hold the action "move_forward"
    Then the camera x position should eventually be above 0.5 within 1.0 seconds
    When I export the trace artifact "live-movement-trace"
    Then the trace artifact should exist
    And the trace artifact should contain "press_action"

  Scenario: Live metrics reflect the actual frame cadence
    Given a live automation session is started in headless mode
    And the simulation tick rate is 10 Hz
    And the generator "Cube" is loaded
    When I reset the metrics window
    Then the metrics window should eventually contain at least 12 samples within 3.0 seconds
    And the average FPS should be below 15