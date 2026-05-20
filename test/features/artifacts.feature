@gpu @trace
Feature: Renderer artifacts over automation gRPC

  Scenario Outline: Capture GPU-rendered screenshot artifact for generator "<generator>" in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "<generator>" is loaded
    And the automation session becomes render-ready
    When I capture the screenshot artifact "<artifact>"
    Then the screenshot artifact should exist

    Examples:
      | generator             | artifact             |
      | Cube                  | cube                 |
      | Perlin Terrain        | perlin-terrain       |
      | monu1                 | monu1                |
      | monu6 without water   | monu6-without-water  |
      | monu8 without water   | monu8-without-water  |
      | buddha 16k            | buddha-16k           |