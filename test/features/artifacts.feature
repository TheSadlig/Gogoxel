@gpu
Feature: Renderer artifacts over automation gRPC

  Scenario: Capture a GPU-rendered screenshot in hidden-window mode
    Given an automation session is started in hidden-window mode
    And the engine is reset to a clean state
    And the simulation tick rate is 60 Hz
    And the generator "Cube" is loaded
    And the automation session becomes render-ready
    When I capture the screenshot artifact "cube"
    Then the screenshot artifact should exist