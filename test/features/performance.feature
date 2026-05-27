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

  @transferqueue
  Scenario: Stream chunk uploads through the transfer queue while using gigabuffer storage and explicit-stack traversal
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the camera is set to x -160 y 224 z 320 yaw 45 pitch -24 fov 60
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes render-ready
    When I reset the metrics window
    And I advance the simulation by 120 frames
    And the automation session becomes streaming-settled
    And I write the metrics artifact "transfer-gigabuffer-stack_perf"
    And I capture the screenshot artifact "transfer-gigabuffer-stack"
    Then the metrics artifact should exist
    And the screenshot artifact should exist
    And the metrics window should contain at least 30 samples
    And the transfer upload path should be "dedicated-transfer-queue" or "graphics-fallback"
    And the transfer queue upload count should be above 0 when a dedicated transfer queue is available
    And the chunk storage should use the renderer gigabuffer
    And the traversal algorithm should be "explicit-stack"