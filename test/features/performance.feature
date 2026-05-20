@gpu @perf
Feature: Renderer metrics over automation gRPC

  Scenario: Record a focused render metrics window
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the automation session becomes render-ready
    When I reset the metrics window
    And I advance the simulation by 120 frames
    Then the metrics window should contain at least 120 samples
    And the average FPS should be above 1
    And the renderer device name should not be empty