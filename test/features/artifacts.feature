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
      | Perlin Terrain        | perlin-terrain       | 500    | 
      | monu1                 | monu1                | 30     | 
      | monu6 without water   | monu6-without-water  | 30     | 
      | monu8 without water   | monu8-without-water  | 30     | 
      | buddha 16k            | buddha-16k           | 30     | 