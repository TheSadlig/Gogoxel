#version 450
#extension GL_EXT_samplerless_texture_functions : require

layout(location = 0) in vec2 uv;
layout(push_constant) uniform CameraBlock {
    vec4 pos;
    vec4 forward;
    vec4 right;
    vec4 up;
    vec4 occupiedMin;
    vec4 occupiedMax;
    float aspect;
    float fovScale;
} camera;

layout(location = 0) out vec4 outColor;

struct SVONode {
    uint payload;
    uint childPointer;
};

layout(std430, binding = 0) readonly buffer SVOBuffer {
    uint svoSize;
    uint nodeCount;
    uint rawWords[];
} svo;

// Brick pool is accessed via texelFetch only (integer coordinates, no filtering),
// so it is bound as a sampled image (utexture3D) rather than a combined-image-sampler.
layout(binding = 1) uniform utexture3D brickPoolTexture;

const int MaxTraversalDepth = 24;
// 64 is enough for natural terrain at the world scale we render: it bounds
// the macro-step inner loop while still walking the longest sky-grazing
// rays. Anything higher just burns ALU on misses.
const int MaxMacroSteps = 64;
const int MaxBrickSteps = 24;
const float BoundaryEpsilon = 1e-4;

const uint AXIS_X = 1u;
const uint AXIS_Y = 2u;
const uint AXIS_Z = 4u;

const uint BrickLeafFlag = 0x80000000u;
const uint ChildMaskMask = 0xFFu;
const uint BrickSize = 8u;
const uint BrickPoolGridEdge = 64u;

uint resolveFaceAxis(uint axisMask, vec3 rd) {
    float best = -1.0;
    uint axis = AXIS_X;

    if ((axisMask & AXIS_X) != 0u) {
        best = abs(rd.x);
        axis = AXIS_X;
    }
    if ((axisMask & AXIS_Y) != 0u && abs(rd.y) >= best) {
        best = abs(rd.y);
        axis = AXIS_Y;
    }
    if ((axisMask & AXIS_Z) != 0u && abs(rd.z) >= best) {
        axis = AXIS_Z;
    }

    return axis;
}

uint dominantAxis(vec3 rd) {
    return resolveFaceAxis(AXIS_X | AXIS_Y | AXIS_Z, rd);
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

vec3 octantOffset(float childSize, uint octant) {
    return vec3(
        (octant & AXIS_X) != 0u ? childSize : 0.0,
        (octant & AXIS_Y) != 0u ? childSize : 0.0,
        (octant & AXIS_Z) != 0u ? childSize : 0.0
    );
}

uvec3 unmirrorNodeOrigin(vec3 mirroredOrigin, float nodeSize, float sceneSize, bvec3 raySign) {
    vec3 actualOrigin = mirroredOrigin;
    if (raySign.x) {
        actualOrigin.x = sceneSize - mirroredOrigin.x - nodeSize;
    }
    if (raySign.y) {
        actualOrigin.y = sceneSize - mirroredOrigin.y - nodeSize;
    }
    if (raySign.z) {
        actualOrigin.z = sceneSize - mirroredOrigin.z - nodeSize;
    }
    return uvec3(actualOrigin);
}

uint exitAxisMask(vec3 exitTs, float exitT) {
    float axisEpsilon = max(BoundaryEpsilon, abs(exitT) * 1e-6);
    uint axisMask = 0u;
    if (abs(exitTs.x - exitT) <= axisEpsilon) {
        axisMask |= AXIS_X;
    }
    if (abs(exitTs.y - exitT) <= axisEpsilon) {
        axisMask |= AXIS_Y;
    }
    if (abs(exitTs.z - exitT) <= axisEpsilon) {
        axisMask |= AXIS_Z;
    }
    return axisMask;
}

float exitTBias(float exitT) {
    return max(BoundaryEpsilon, exitT * 1e-6);
}

bool pointInsideNode(vec3 point, vec3 origin, float size) {
    vec3 epsilon = vec3(BoundaryEpsilon);
    return all(greaterThanEqual(point, origin - epsilon)) &&
        all(lessThan(point, origin + vec3(size)));
}

SVONode getSvoNode(uint index) {
    SVONode node;
    uint baseWord = index * 2u;
    node.payload = svo.rawWords[baseWord];
    node.childPointer = svo.rawWords[baseWord + 1u];
    return node;
}

uint nodeChildMask(SVONode node) {
    return node.payload & ChildMaskMask;
}

uint nodeMaterialID(SVONode node) {
    return (node.payload >> 8u) & 0x7FFFFFu;
}

bool isBrickLeaf(SVONode node) {
    return (node.payload & BrickLeafFlag) != 0u;
}

bool isSolidLeaf(SVONode node) {
    return !isBrickLeaf(node) && node.childPointer == 0u && nodeChildMask(node) == 1u;
}

uint paletteColorForMaterial(uint materialID) {
    if (materialID == 0u) {
        return 0u;
    }
    return svo.rawWords[svo.nodeCount * 2u + materialID];
}

vec4 shadeMaterial(uint materialID, vec3 normal) {
    uint packedColor = paletteColorForMaterial(materialID);
    // CPU packs palette as: R | G<<8 | B<<16 | 0xFF000000.
    // In little-endian memory that's bytes [R, G, B, 0xFF].
    // unpackUnorm4x8 returns (byte0, byte1, byte2, byte3) in xyzw = (R, G, B, A).
    vec4 voxelColor = unpackUnorm4x8(packedColor);
    // Z is the height (up) axis.  Light comes from above (+Z) with a slight
    // north-east tilt.  Using max(0,dot)*diffuse + ambient keeps all faces
    // visible while avoiding harsh near-white Y-face speckling.
    vec3 lightDir = normalize(vec3(0.6, 0.8, 1.0));
    float diffuse = max(0.0, dot(normal, lightDir));
    float lighting = 0.72 + diffuse * 0.18;
    return vec4(voxelColor.rgb * lighting, 1.0);
}

uvec3 brickSlotCoord(uint slot) {
    return uvec3(
        slot % BrickPoolGridEdge,
        (slot / BrickPoolGridEdge) % BrickPoolGridEdge,
        slot / (BrickPoolGridEdge * BrickPoolGridEdge)
    );
}

uint brickBoundaryFaceMask(vec3 localPos, vec3 rd) {
    uint mask = 0u;
    float minEdge = BoundaryEpsilon * 2.0;
    float maxEdge = float(BrickSize) - minEdge;

    if ((localPos.x <= minEdge && rd.x > 0.0) || (localPos.x >= maxEdge && rd.x < 0.0)) {
        mask |= AXIS_X;
    }
    if ((localPos.y <= minEdge && rd.y > 0.0) || (localPos.y >= maxEdge && rd.y < 0.0)) {
        mask |= AXIS_Y;
    }
    if ((localPos.z <= minEdge && rd.z > 0.0) || (localPos.z >= maxEdge && rd.z < 0.0)) {
        mask |= AXIS_Z;
    }

    return mask;
}

bool raymarchBrick(
    uint slot,
    uvec3 brickOriginWorld,
    vec3 currPosWorld,
    vec3 rd,
    uint entryFaceMask,
    bool hasEntryFaceMask,
    out uint hitMaterialID,
    out vec3 hitNormal
) {
    if (slot == 0u) {
        return false;
    }

    ivec3 brickOriginPool = ivec3(brickSlotCoord(slot) * BrickSize);
    vec3 localPos = currPosWorld - vec3(brickOriginWorld);
    uint boundaryFaceMask = brickBoundaryFaceMask(localPos, rd);
    localPos = clamp(localPos, vec3(0.0), vec3(float(BrickSize) - 1e-4));

    ivec3 voxel = ivec3(floor(localPos));
    ivec3 step = ivec3(
        rd.x > 0.0 ? 1 : (rd.x < 0.0 ? -1 : 0),
        rd.y > 0.0 ? 1 : (rd.y < 0.0 ? -1 : 0),
        rd.z > 0.0 ? 1 : (rd.z < 0.0 ? -1 : 0)
    );
    vec3 invDir = 1.0 / (rd + sign(rd) * 1e-6);
    vec3 nextBoundary = vec3(voxel) + vec3(
        rd.x > 0.0 ? 1.0 : 0.0,
        rd.y > 0.0 ? 1.0 : 0.0,
        rd.z > 0.0 ? 1.0 : 0.0
    );
    vec3 tMax = (nextBoundary - localPos) * invDir;
    vec3 tDelta = abs(invDir);
    if (step.x == 0) {
        tMax.x = 1.0 / 0.0;
        tDelta.x = 1.0 / 0.0;
    }
    if (step.y == 0) {
        tMax.y = 1.0 / 0.0;
        tDelta.y = 1.0 / 0.0;
    }
    if (step.z == 0) {
        tMax.z = 1.0 / 0.0;
        tDelta.z = 1.0 / 0.0;
    }

    uint faceMask = boundaryFaceMask != 0u ? boundaryFaceMask : entryFaceMask;
    bool hasFaceMask = boundaryFaceMask != 0u || hasEntryFaceMask;

    for (int stepIdx = 0; stepIdx < MaxBrickSteps; stepIdx++) {
        if (any(lessThan(voxel, ivec3(0))) || any(greaterThanEqual(voxel, ivec3(int(BrickSize))))) {
            return false;
        }

        uint materialID = texelFetch(brickPoolTexture, brickOriginPool + voxel, 0).r;
        if (materialID > 0u) {
            // Cheap DDA normal: the face we entered the voxel through is
            // already tracked via faceMask (set from the previous step's
            // exit axis, or the brick entry-face mask for the first voxel).
            // This replaces a 12-tap gradient probe per hit with a single
            // axis lookup — identical visual result for axis-aligned voxels.
            uint hitAxis = hasFaceMask ? resolveFaceAxis(faceMask, rd) : dominantAxis(rd);
            hitNormal = axisNormal(hitAxis, rd);
            hitMaterialID = materialID;
            return true;
        }

        float nextT = min(tMax.x, min(tMax.y, tMax.z));
        float axisEpsilon = max(BoundaryEpsilon, abs(nextT) * 1e-6);
        uint exitMask = 0u;
        if (abs(tMax.x - nextT) <= axisEpsilon) {
            exitMask |= AXIS_X;
        }
        if (abs(tMax.y - nextT) <= axisEpsilon) {
            exitMask |= AXIS_Y;
        }
        if (abs(tMax.z - nextT) <= axisEpsilon) {
            exitMask |= AXIS_Z;
        }

        if ((exitMask & AXIS_X) != 0u) {
            voxel.x += step.x;
            tMax.x += tDelta.x;
        }
        if ((exitMask & AXIS_Y) != 0u) {
            voxel.y += step.y;
            tMax.y += tDelta.y;
        }
        if ((exitMask & AXIS_Z) != 0u) {
            voxel.z += step.z;
            tMax.z += tDelta.z;
        }

        faceMask = exitMask != 0u ? exitMask : dominantAxis(rd);
        hasFaceMask = true;
    }

    return false;
}

bool intersectSceneBounds(vec3 ro, vec3 rd, out float tMin, out float tMax, out uint entryAxis, out bool hasEntryFace) {
    vec3 boxMin = camera.occupiedMin.xyz;
    vec3 boxMax = camera.occupiedMax.xyz;
    if (any(lessThanEqual(boxMax, boxMin))) {
        return false;
    }

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
    float sceneSize = float(svo.svoSize);

    bvec3 raySign = lessThan(rd, vec3(0.0));
    uint octantMask = (raySign.x ? AXIS_X : 0u) |
        (raySign.y ? AXIS_Y : 0u) |
        (raySign.z ? AXIS_Z : 0u);

    vec3 mirroredRd = abs(rd);
    vec3 invDir = 1.0 / max(mirroredRd, vec3(1e-20));
    vec3 mirroredRo = ro;
    if (raySign.x) {
        mirroredRo.x = sceneSize - mirroredRo.x;
    }
    if (raySign.y) {
        mirroredRo.y = sceneSize - mirroredRo.y;
    }
    if (raySign.z) {
        mirroredRo.z = sceneSize - mirroredRo.z;
    }

    uint nodeStack[MaxTraversalDepth];
    vec3 originStack[MaxTraversalDepth];
    float sizeStack[MaxTraversalDepth];
    int stackDepth = 1;
    nodeStack[0] = 0u;
    originStack[0] = vec3(0.0);
    sizeStack[0] = sceneSize;

    for (int stepIdx = 0; stepIdx < MaxMacroSteps; stepIdx++) {
        if (t >= tMax) {
            break;
        }

        vec3 currentPos = mirroredRo + mirroredRd * t;
        while (stackDepth > 1 && !pointInsideNode(currentPos, originStack[stackDepth - 1], sizeStack[stackDepth - 1])) {
            stackDepth--;
        }
        if (!pointInsideNode(currentPos, originStack[0], sizeStack[0])) {
            break;
        }

        vec3 exitOrigin = originStack[stackDepth - 1];
        float exitSize = sizeStack[stackDepth - 1];

        for (int depth = stackDepth - 1; depth < MaxTraversalDepth; depth++) {
            uint currentNodeIdx = nodeStack[stackDepth - 1];
            vec3 currentOrigin = originStack[stackDepth - 1];
            float currentSize = sizeStack[stackDepth - 1];
            SVONode currentNode = getSvoNode(currentNodeIdx);
            uint childMask = nodeChildMask(currentNode);

            if (currentNodeIdx == 0u && childMask == 0u && currentNode.childPointer == 0u && !isBrickLeaf(currentNode)) {
                return vec4(0.0, 0.0, 0.0, 0.0);
            }

            if (isBrickLeaf(currentNode)) {
                uint hitMaterialID = 0u;
                vec3 hitNormal = vec3(0.0);
                uvec3 brickOrigin = unmirrorNodeOrigin(currentOrigin, currentSize, sceneSize, raySign);
                if (currentNode.childPointer == 0u) {
                    uint fallbackMaterialID = nodeMaterialID(currentNode);
                    if (fallbackMaterialID > 0u) {
                        uint hitAxis = hasFaceMask ? resolveFaceAxis(faceMask, rd) : dominantAxis(rd);
                        return shadeMaterial(fallbackMaterialID, axisNormal(hitAxis, rd));
                    }
                    exitOrigin = currentOrigin;
                    exitSize = currentSize;
                    break;
                }
                if (raymarchBrick(currentNode.childPointer, brickOrigin, ro + rd * t, rd, faceMask, hasFaceMask, hitMaterialID, hitNormal)) {
                    return shadeMaterial(hitMaterialID, hitNormal);
                }
                exitOrigin = currentOrigin;
                exitSize = currentSize;
                break;
            }

            if (isSolidLeaf(currentNode)) {
                uint hitAxis = hasFaceMask ? resolveFaceAxis(faceMask, rd) : dominantAxis(rd);
                return shadeMaterial(nodeMaterialID(currentNode), axisNormal(hitAxis, rd));
            }

            if (currentNode.childPointer == 0u || childMask == 0u || currentSize <= 1.0) {
                exitOrigin = currentOrigin;
                exitSize = currentSize;
                break;
            }

            float childSize = currentSize * 0.5;
            vec3 tMid = (currentOrigin + vec3(childSize) - mirroredRo) * invDir;
            uint octant = 0u;
            if (t >= tMid.x) {
                octant |= AXIS_X;
            }
            if (t >= tMid.y) {
                octant |= AXIS_Y;
            }
            if (t >= tMid.z) {
                octant |= AXIS_Z;
            }

            vec3 childOrigin = currentOrigin + octantOffset(childSize, octant);

            uint realOctant = octant ^ octantMask;
            if ((childMask & (1u << realOctant)) == 0u) {
                exitOrigin = childOrigin;
                exitSize = childSize;
                break;
            }

            uint maskBefore = childMask & ((1u << realOctant) - 1u);
            if (stackDepth >= MaxTraversalDepth) {
                exitOrigin = childOrigin;
                exitSize = childSize;
                break;
            }
            nodeStack[stackDepth] = currentNode.childPointer + bitCount(maskBefore);
            originStack[stackDepth] = childOrigin;
            sizeStack[stackDepth] = childSize;
            stackDepth++;
        }

        vec3 exitTs = (exitOrigin + vec3(exitSize) - mirroredRo) * invDir;
        float exitT = min(exitTs.x, min(exitTs.y, exitTs.z));
        uint exitMask = exitAxisMask(exitTs, exitT);
        faceMask = exitMask != 0u ? exitMask : dominantAxis(rd);
        hasFaceMask = true;
        float nextT = exitT + exitTBias(exitT);
        if (nextT <= t) {
            break;
        }

        t = nextT;
        tMin = t;
    }

    return vec4(0.0, 0.0, 0.0, 0.0);
}

void main() {
    vec2 screen = uv * 2.0 - 1.0;
    screen.x *= camera.aspect;
    screen *= camera.fovScale;
    vec3 rayDir = normalize(camera.forward.xyz + camera.right.xyz * screen.x + camera.up.xyz * screen.y);
    vec4 color = raymarchVoxels(camera.pos.xyz, rayDir);
    // Discard fully-transparent (miss) fragments so the framebuffer keeps the
    // clear colour without a write. This also lets the GPU skip any per-sample
    // blending work for misses, which dominate sparse scenes.
    if (color.a == 0.0) {
        discard;
    }
    outColor = color;
}
