@gpu @trace
Feature: Renderer artifacts over automation gRPC

  Scenario Outline: Capture GPU-rendered screenshot artifact for generator "<generator>" in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "<generator>" is loaded
    And the automation session becomes render-ready
    And I advance the simulation by <frames> frames
    When I capture the screenshot artifact "<artifact>"
    Then the screenshot artifact should exist

    Examples:
      | generator             | artifact             | frames |
      | Cube                  | cube                 | 30     | 
      | monu1                 | monu1                | 30     | 
      | monu6 without water   | monu6-without-water  | 30     | 
      | monu8 without water   | monu8-without-water  | 30     | 
      | buddha 16k            | buddha-16k           | 30     |

  Scenario: Capture streaming-settled GPU-rendered screenshot artifact for Perlin Terrain in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    And the rendered view should remain visually stable for 3 frames
    When I reset the metrics window
    And I advance the simulation by 60 frames
    And I write the metrics artifact "perlin-terrain_perf"
    And I capture the screenshot artifact "perlin-terrain"
    Then the metrics artifact should exist
    And the metrics window should contain at least 30 samples
    And the average FPS should be above 1
    And the screenshot artifact should exist

  Scenario: Capture GPU-rendered screenshot artifact for Perlin Terrain from a low explicit camera in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the camera is set to x -160 y 224 z 320 yaw 45 pitch -24 fov 60
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    When I capture the screenshot artifact "perlin-low-altitude"
    Then the world size should be at least 384
    And the screenshot artifact should contain at least 10 percent non-background pixels
    And the screenshot artifact should exist

  Scenario: Capture GPU-rendered large-overview screenshot artifact for Perlin Terrain in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the camera is set to x 512 y 1536 z 1400 yaw 270 pitch -50 fov 70
    And the generator "Perlin Terrain" is loaded
    And the automation session becomes streaming-settled
    And the rendered view should remain visually stable for 3 frames
    When I capture the screenshot artifact "perlin-overview"
    Then the world size should be at least 512
    And the screenshot artifact should contain at least 10 percent non-background pixels
    And the screenshot artifact should exist
