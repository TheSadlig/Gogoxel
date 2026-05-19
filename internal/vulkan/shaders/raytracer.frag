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
    uint svoSize;
    uint _pad;
    uint rawNodes[];
} svo;

const int MaxDepth = 10; // Log2(1024)
const float BoundaryEpsilon = 1e-4;

const uint AXIS_X = 1u;
const uint AXIS_Y = 2u;
const uint AXIS_Z = 4u;

uint resolveFaceAxis(uint axisMask, vec3 rd) {
    float best = -1.0;
    uint axis = AXIS_X;

    if ((axisMask & AXIS_X) != 0u) {
        best = abs(rd.x);
        axis = AXIS_X;
    }
    if ((axisMask & AXIS_Y) != 0u && abs(rd.y) > best) {
        best = abs(rd.y);
        axis = AXIS_Y;
    }
    if ((axisMask & AXIS_Z) != 0u && abs(rd.z) > best) {
        axis = AXIS_Z;
    }

    return axis;
}

vec3 axisNormal(uint axis, vec3 rd) {
    if (axis == AXIS_X) {
        return vec3(rd.x > 0.0 ? -1.0 : 1.0, 0.0, 0.0);
    }
    if (axis == AXIS_Y) {
        return vec3(0.0, rd.y > 0.0 ? -1.0 : 1.0, 0.0);
    }
    return vec3(0.0, 0.0, rd.z > 0.0 ? -1.0 : 1.0);
}

SVONode getSvoNode(uint index) {
    SVONode node;
    uint baseWord = index * 2u;
    node.childMaskAndColor = svo.rawNodes[baseWord];
    node.childPointer = svo.rawNodes[baseWord + 1u];
    return node;
}

bool intersectSceneBounds(vec3 ro, vec3 rd, out float tMin, out float tMax, out uint entryAxis, out bool hasEntryFace) {
    float sceneSize = max(float(svo.svoSize), 1.0);
    vec3 boxMin = vec3(0.0);
    vec3 boxMax = vec3(sceneSize);
    vec3 invDir = 1.0 / (rd + sign(rd) * 1e-6);
    vec3 t0 = (boxMin - ro) * invDir;
    vec3 t1 = (boxMax - ro) * invDir;
    vec3 t0_min = min(t0, t1);
    vec3 t1_max = max(t0, t1);
    tMin = max(max(t0_min.x, t0_min.y), t0_min.z);
    tMax = min(min(t1_max.x, t1_max.y), t1_max.z);

    entryAxis = AXIS_X;
    float entryT = t0_min.x;
    if (t0_min.y > entryT) {
        entryT = t0_min.y;
        entryAxis = AXIS_Y;
    }
    if (t0_min.z > entryT) {
        entryAxis = AXIS_Z;
    }
    hasEntryFace = tMin >= 0.0;

    return tMin <= tMax && tMax >= 0.0;
}

vec4 raymarchVoxels(vec3 ro, vec3 rd) {
    float tMin, tMax;
    uint entryAxis = AXIS_X;
    bool hasEntryFace = false;
    if (!intersectSceneBounds(ro, rd, tMin, tMax, entryAxis, hasEntryFace)) {
        return vec4(0.0, 0.0, 0.0, 0.0);
    }

    uint faceMask = hasEntryFace ? entryAxis : 0u;
    bool hasFaceMask = hasEntryFace;

    float t = max(tMin, 0.0);
    vec3 stepDir = sign(rd);
    vec3 invDir = 1.0 / (rd + sign(rd) * 1e-6);

    uint nodeStack[MaxDepth + 1];
    uvec3 posStack[MaxDepth + 1];
    uint sizeStack[MaxDepth + 1];

    int depth = 0;
    nodeStack[0] = 0u;
    posStack[0]  = uvec3(0u);
    sizeStack[0] = svo.svoSize;

    vec3 hitNormal = vec3(0.0);
    
    // Compute starting point inside the bounding volume
    vec3 currPos = ro + rd * t;

    for (int stepIdx = 0; stepIdx < 500; stepIdx++) {
        if (t >= tMax || depth < 0) break;

        uint currentNodeIdx = nodeStack[depth];
        uvec3 currentOrigin = posStack[depth];
        uint currentSize    = sizeStack[depth];
        
        currPos = clamp(currPos, vec3(0.0), vec3(float(svo.svoSize) - 1e-4));

        uvec3 center = currentOrigin + uvec3(currentSize >> 1u);
        vec3 classifyPos = currPos;
        if (abs(classifyPos.x - float(center.x)) <= BoundaryEpsilon) {
            classifyPos.x += (stepDir.x < 0.0 ? -BoundaryEpsilon : BoundaryEpsilon);
        }
        if (abs(classifyPos.y - float(center.y)) <= BoundaryEpsilon) {
            classifyPos.y += (stepDir.y < 0.0 ? -BoundaryEpsilon : BoundaryEpsilon);
        }
        if (abs(classifyPos.z - float(center.z)) <= BoundaryEpsilon) {
            classifyPos.z += (stepDir.z < 0.0 ? -BoundaryEpsilon : BoundaryEpsilon);
        }

        uint octantX = (classifyPos.x >= float(center.x)) ? 1u : 0u;
        uint octantY = (classifyPos.y >= float(center.y)) ? 2u : 0u;
        uint octantZ = (classifyPos.z >= float(center.z)) ? 4u : 0u;
        uint currentOctant = octantX | octantY | octantZ;
        
        SVONode currentNode = getSvoNode(currentNodeIdx);
        uint childMask = currentNode.childMaskAndColor & 0xFFu;

        if (currentNodeIdx == 0u && childMask == 0u) {
            return vec4(0.0, 0.0, 0.0, 0.0);
        }

        uint halfSize = currentSize >> 1u;
        
        uvec3 octantXYZ = uvec3(octantX & 1u, (octantY >> 1u) & 1u, (octantZ >> 2u) & 1u);
        uvec3 targetOctantCoord = octantXYZ + uvec3(stepDir.x > 0.0 ? 1u : 0u, stepDir.y > 0.0 ? 1u : 0u, stepDir.z > 0.0 ? 1u : 0u);
        vec3 targetFace = vec3(currentOrigin) + vec3(halfSize) * vec3(targetOctantCoord);

        vec3 tBounds = (targetFace - currPos) * invDir;

        if (stepDir.x == 0.0) tBounds.x = 1.0 / 0.0;
        if (stepDir.y == 0.0) tBounds.y = 1.0 / 0.0;
        if (stepDir.z == 0.0) tBounds.z = 1.0 / 0.0;

        uint exitAxis = AXIS_X;
        float tStep = tBounds.x;
        
        if (tBounds.y < tStep) { tStep = tBounds.y; exitAxis = AXIS_Y; }
        if (tBounds.z < tStep) { tStep = tBounds.z; exitAxis = AXIS_Z; }

        float axisEpsilon = max(BoundaryEpsilon, abs(tStep) * 1e-6);
        uint exitMask = 0u;
        if (abs(tBounds.x - tStep) <= axisEpsilon) { exitMask |= AXIS_X; }
        if (abs(tBounds.y - tStep) <= axisEpsilon) { exitMask |= AXIS_Y; }
        if (abs(tBounds.z - tStep) <= axisEpsilon) { exitMask |= AXIS_Z; }

        // HIT CONDITION: Solid terminal leaf found!
        if (currentNode.childPointer == 0u || depth == MaxDepth) {
            uint hitAxis = hasFaceMask ? resolveFaceAxis(faceMask, rd) : exitAxis;
            hitNormal = axisNormal(hitAxis, rd);

            uint packedColor = currentNode.childMaskAndColor >> 8u;
            vec4 voxelColor = unpackUnorm4x8(packedColor).abgr;
            float lighting = dot(hitNormal, normalize(vec3(0.5, 1.0, 0.3))) * 0.5 + 0.5;
            return vec4(voxelColor.rgb * lighting, 1.0);
        }

        bool hasChild = ((childMask >> currentOctant) & 1u) == 1u;

        if (hasChild) {
            uint childPtr = currentNode.childPointer;
            uint maskBefore = childMask & ((1u << currentOctant) - 1u);
            uint memoryOffset = bitCount(maskBefore);
            uint nextNodeIdx = childPtr + memoryOffset;

            uvec3 childOrigin = currentOrigin + uvec3(
                currentOctant & 1u,
                (currentOctant >> 1u) & 1u,
                (currentOctant >> 2u) & 1u
            ) * halfSize;

            depth++;
            nodeStack[depth] = nextNodeIdx;
            posStack[depth]  = childOrigin;
            sizeStack[depth] = halfSize;
            continue;
        }

        // ADVANCE MECHANIC: Step directly to the exit face edge
        t += tStep;
        currPos += rd * tStep;
        faceMask = exitMask != 0u ? exitMask : exitAxis;
        hasFaceMask = true;

        // Force a physical bit-accurate snap over the crossed plane face
        // to securely jump into the neighboring voxel area without infinite loop grid-locks
        if ((exitMask & AXIS_X) != 0u) {
            currPos.x = targetFace.x + (stepDir.x * 1e-3);
        }
        if ((exitMask & AXIS_Y) != 0u) {
            currPos.y = targetFace.y + (stepDir.y * 1e-3);
        }
        if ((exitMask & AXIS_Z) != 0u) {
            currPos.z = targetFace.z + (stepDir.z * 1e-3);
        }

        // POP MECHANIC: Backtrack up the tree hierarchy frames until current snapped position matches parent bounds
        while (depth >= 0) {
            uvec3 pMin = posStack[depth];
            uvec3 pMax = pMin + uvec3(sizeStack[depth]);
            
            if (currPos.x >= float(pMin.x) && currPos.x < float(pMax.x) &&
                currPos.y >= float(pMin.y) && currPos.y < float(pMax.y) &&
                currPos.z >= float(pMin.z) && currPos.z < float(pMax.z)) {
                break; 
            }
            depth--;
        }
    }

    return vec4(0.0, 0.0, 0.0, 0.0);
}

void main() {
    vec2 screen = uv * 2.0 - 1.0;
    screen.x *= camera.aspect;
    screen *= camera.fovScale;
    vec3 rayDir = normalize(camera.forward.xyz + camera.right.xyz * screen.x + camera.up.xyz * screen.y);
    outColor = raymarchVoxels(camera.pos.xyz, rayDir);
}
