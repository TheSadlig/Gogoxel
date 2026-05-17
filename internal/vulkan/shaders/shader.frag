#version 450

layout(location = 0) in vec2 uv;
layout(location = 0) out vec4 outColor;

void main() {
    float wave = 0.5 + 0.5 * sin(uv.x * 12.0) * cos(uv.y * 12.0);
    vec3 baseA = vec3(0.08, 0.12, 0.20);
    vec3 baseB = vec3(0.95, 0.42, 0.20);
    vec3 accent = vec3(0.15, 0.72, 0.96);

    vec3 color = mix(baseA, baseB, uv.x);
    color = mix(color, accent, uv.y);
    color += 0.15 * wave;

    outColor = vec4(color, 1.0);
}