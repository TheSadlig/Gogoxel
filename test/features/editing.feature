@editing @trace
Feature: Interactive voxel editing over automation gRPC

  Scenario: Add and remove cubes through the cursor ray in headless mode
    Given an automation session is started in headless mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the camera is set to x 0 y 64 z 64 yaw 0 pitch 0 fov 60
    And the selected cube material is "grass"
    When I place a cube through the centered cursor ray
    Then the last cursor edit should target voxel x 31 y 64 z 64
    And I remove a cube through the centered cursor ray
    And the last cursor edit should target voxel x 31 y 64 z 64
    And I advance the simulation by 4 ticks
    And I write the metrics artifact "editing-headless_perf"
    Then the metrics artifact should exist

  Scenario: Holding place while moving paints a multi-voxel stroke
    Given an automation session is started in headless mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the camera is set to x 0 y 64 z 64 yaw 0 pitch 0 fov 60
    And the selected cube material is "grass"
    When I hold the action "place_cube"
    And I hold the action "move_up"
    And I advance the simulation by 100 ticks
    And I release the action "move_up"
    And I release the action "place_cube"
    Then the current scene should contain at least 2 bricks
    And I write the metrics artifact "editing-stroke_perf"
    Then the metrics artifact should exist

  @gpu @perf
  Scenario: Edited cubes become visible without collapsing frame pacing
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the automation session becomes render-ready
    And the camera is set to x 0 y 64 z 64 yaw 0 pitch 0 fov 60
    And the selected cube material is "sand"
    When I reset the metrics window
    And I place a cube through the centered cursor ray
    And the last cursor edit should target voxel x 31 y 64 z 64
    And I advance the simulation by 30 frames
    And I capture the screenshot artifact "editing-visible"
    And I write the metrics artifact "editing-visible_perf"
    Then the screenshot artifact should exist
    And the screenshot artifact should contain at least 5 percent non-background pixels
    And the metrics artifact should exist
    And the metrics window should contain at least 30 samples
    And the average FPS should be above 1

  @gpu @live
  Scenario: Edited cubes remain visible in a live hidden-window session
    Given a live automation session is started in hidden-window mode
    And the generator "Cube" is loaded
    And the automation session becomes render-ready
    And the camera is set to x 0 y 64 z 64 yaw 0 pitch 0 fov 60
    And the selected cube material is "sand"
    And I reset the metrics window
    When I place a cube through the centered cursor ray
    Then the metrics window should eventually contain at least 30 samples within 2.0 seconds
    And I capture the screenshot artifact "editing-visible-live"
    Then the screenshot artifact should exist
    And the screenshot artifact should contain at least 5 percent non-background pixels

  @gpu
  Scenario: Removing a cube top voxel preserves neighboring brick regions
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the automation session becomes render-ready
    And the camera is set to x 0 y 64 z 128 yaw 0 pitch -26.565 fov 60
    When I reset the metrics window
    And I remove a cube through the centered cursor ray
    Then the last cursor edit should have changed the scene
    And I advance the simulation by 30 frames
    And I capture the screenshot artifact "editing-remove-top-visible"
    Then the screenshot artifact should exist
    And the screenshot artifact should contain at least 5 percent non-background pixels

  @gpu
  Scenario: Terrain edits remain visible after streaming settles
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    And the selected cube material is "sand"
    When I reset the metrics window
    And I place a cube through the centered cursor ray
    Then the last cursor edit should have changed the scene
    And I advance the simulation by 120 frames
    And I capture the screenshot artifact "terrain-edit-visible"
    Then the screenshot artifact should exist
    And the screenshot artifact should contain at least 5 percent non-background pixels

  @gpu @perf
  Scenario: Terrain stroke keeps frame pacing responsive after streaming settles
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    And the selected cube material is "sand"
    When I reset the metrics window
    And I hold the action "place_cube"
    And I hold the action "move_up"
    And I advance the simulation by 90 ticks
    And I release the action "move_up"
    And I release the action "place_cube"
    And I advance the simulation by 60 frames
    And I write the metrics artifact "terrain-stroke-responsive_perf"
    Then the metrics artifact should exist
    And the metrics window should contain at least 60 samples
    And the average FPS should be above 1

  @gpu
  Scenario: Switching from terrain to cube does not exhaust brick-upload staging
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    When the generator "Cube" is loaded
    And the automation session becomes render-ready
    And I advance the simulation by 60 frames
    And I capture the screenshot artifact "switch-perlin-cube-visible"
    Then the screenshot artifact should exist
    And the screenshot artifact should contain at least 5 percent non-background pixels