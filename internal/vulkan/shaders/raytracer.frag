#version 450

layout(location = 0) in vec2 uv;
layout(push_constant) uniform CameraBlock {
    vec4 pos;
    vec4 forward;
    vec4 right;
    vec4 up;
    float aspect;
    float fovScale;
} camera;

layout(location = 0) out vec4 outColor;

struct SVONode {
    uint childMaskAndColor; // Bits 0-7: Child mask // Bit 8-31: color index
    
    uint childPointer;      // Index du premier enfant dans le tableau global
};

layout(std430, binding = 0) readonly buffer SVOBuffer {
    SVONode nodes[];
} svo;

const uint StartNodeSize = 1024;
const int MaxDepth = 10; // Log2(1024)

// Intersection classique Ray-AABB pour déterminer l'entrée du rayon dans l'Octree
bool intersectSceneBounds(vec3 ro, vec3 rd, out float tMin, out float tMax) {
    vec3 boxMin = vec3(0.0);
    vec3 boxMax = vec3(StartNodeSize);
    vec3 invDir = 1.0 / (rd + sign(rd) * 1e-6); // Évite la division par zéro
    vec3 t0 = (boxMin - ro) * invDir;
    vec3 t1 = (boxMax - ro) * invDir;
    vec3 t0_min = min(t0, t1);
    vec3 t1_max = max(t0, t1);
    tMin = max(max(t0_min.x, t0_min.y), t0_min.z);
    tMax = min(min(t1_max.x, t1_max.y), t1_max.z);
    return tMin <= tMax && tMax >= 0.0;
}

vec4 raymarchVoxels(vec3 ro, vec3 rd) {
    float tMin, tMax;
    if (!intersectSceneBounds(ro, rd, tMin, tMax)) {
        return vec4(0.0, 0.0, 0.0, 0.0);
    }

    float t = max(tMin, 0.0);

    // Handles ray direction
    vec3 stepDir = sign(rd);

    uint nodeStack[MaxDepth + 1];
    vec3 posStack[MaxDepth + 1];
    
    int depth = 0;
    nodeStack[0] = 0;
    posStack[0]  = vec3(0.0);
    float currentSize = StartNodeSize;

    vec3 hitNormal = vec3(0.0);

    for (int stepIdx = 0; stepIdx < 500; stepIdx++) {
        if (t >= tMax || depth < 0) break;

        uint currentNodeIdx = nodeStack[depth];
        vec3 currentOrigin  = posStack[depth];
        
        vec3 currPos = ro + rd * t;

        // Find the octant of the current node that the ray is in
        vec3 center = currentOrigin + vec3(currentSize * 0.5);
        uint octantX = (currPos.x >= center.x) ? 1u : 0u;
        uint octantY = (currPos.y >= center.y) ? 2u : 0u;
        uint octantZ = (currPos.z >= center.z) ? 4u : 0u;
        uint currentOctant = octantX | octantY | octantZ;

        // Determins the smallest t leave the current octant
        vec3 targetFace = currentOrigin + vec3(currentSize * 0.5) * vec3(float(octantX & 1u) + 1.0, float((octantY >> 1u) & 1u) + 1.0, float((octantZ >> 2u) & 1u) + 1.0);
        if (stepDir.x < 0.0) targetFace.x -= currentSize * 0.5;
        if (stepDir.y < 0.0) targetFace.y -= currentSize * 0.5;
        if (stepDir.z < 0.0) targetFace.z -= currentSize * 0.5;

        vec3 tBounds = (targetFace - ro) / (rd + 1e-6);
        float tNext = min(min(tBounds.x, tBounds.y), tBounds.z);

        // Getting child mask, and checking if child is empty
        uint childMask = svo.nodes[currentNodeIdx].childMaskAndColor & 0xFFu;
        bool hasChild = ((childMask >> currentOctant) & 1u) == 1u;

        if (hasChild) {
            uint childPtr = svo.nodes[currentNodeIdx].childPointer;

            if (childPtr == 0u || depth == MaxDepth) {
                // HIT - This is a leaf node

                // Determine normale based on which face we hit
                if (tNext == tBounds.x) hitNormal = vec3(-stepDir.x, 0.0, 0.0);
                else if (tNext == tBounds.y) hitNormal = vec3(0.0, -stepDir.y, 0.0);
                else hitNormal = vec3(0.0, 0.0, -stepDir.z);

                // Getting RGB color from bits 8-31
                uint packedColor = svo.nodes[currentNodeIdx].childMaskAndColor >> 8u;
                vec4 voxelColor = unpackUnorm4x8(packedColor).abgr;

                // Half Lambert lighting
                float lighting = dot(hitNormal, normalize(vec3(0.5, 1.0, 0.3))) * 0.5 + 0.5;
                return vec4(voxelColor.rgb * lighting, 1.0);
            } else {
                // PUSH - descend into the child node
                uint maskBefore = childMask & ((1u << currentOctant) - 1u);
                uint memoryOffset = bitCount(maskBefore);
                uint nextNodeIdx = childPtr + memoryOffset;

                // child cube origin
                vec3 childOrigin = currentOrigin + vec3(
                    float(currentOctant & 1u),
                    float((currentOctant >> 1u) & 1u),
                    float((currentOctant >> 2u) & 1u)
                ) * (currentSize * 0.5);

                // PUSH - descend into the child node
                depth++;
                nodeStack[depth] = nextNodeIdx;
                posStack[depth]  = childOrigin;
                currentSize *= 0.5;
                continue;
            }
        }

        // POP - empty octant, advance to next 
        t = tNext + 0.001;

        // checking if we are still in the current node, otherwise we need to POP
        vec3 nextPos = ro + rd * t;
        while (depth >= 0) {
            vec3 pMin = posStack[depth];
            vec3 pMax = pMin + vec3(currentSize);
            
            // If the new position is still inside this parent, stay at this level
            if (all(greaterThanEqual(nextPos, pMin)) && all(lessThan(nextPos, pMax))) {
                break; 
            }
            
            // Otherwise, pop (ascend)
            depth--;
            currentSize *= 2.0;
        }
    }

    return vec4(0.0, 0.0, 0.0, 0.0); // Le rayon a traversé tout l'arbre sans collision (Air / Vide)
}

void main() {
    vec2 screen = uv * 2.0 - 1.0;
    screen.x *= camera.aspect;
    screen *= camera.fovScale;
    vec3 rayDir = normalize(camera.forward.xyz + camera.right.xyz * screen.x + camera.up.xyz * screen.y);
    outColor = raymarchVoxels(camera.pos.xyz, rayDir);
}