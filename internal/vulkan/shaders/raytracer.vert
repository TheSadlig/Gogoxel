#version 450

layout(location = 0) out vec2 uv;

// passthrough cameraPos
// Define the Push Constant block
layout(push_constant) uniform CameraBlock {
    vec3 cameraPos;
} cameraData;

layout(location = 1) out vec3 cameraPosOut;


vec2 positions[3] = vec2[](
    vec2(-1.0, -1.0),
    vec2(3.0, -1.0),
    vec2(-1.0, 3.0)
);

void main() {
    vec2 position = positions[gl_VertexIndex];
    uv = position * 0.5 + 0.5;
    // cameraPosOut = vec3(1.5, 1.5, 5.0); // Pass the camera position to the fragment shader
    cameraPosOut = cameraData.cameraPos; // Pass the camera position from the push constant
    gl_Position = vec4(position, 0.0, 1.0);
}