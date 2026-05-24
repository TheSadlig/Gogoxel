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
const int MaxMacroSteps = 500;
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
    out vec3 hitNormal,
    out uvec3 hitVoxelWorld
) {
    hitVoxelWorld = uvec3(0u);
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
            // Gradient-based surface normal from 6 axis-aligned neighbours.
            // Prefer a ±2 central-difference stencil: on slopes, adjacent surface
            // voxels alternate between Z-up and side-face normals with a ±1 stencil
            // (the "herringbone" artefact) because staircase steps are 1 voxel tall.
            // A ±2 stencil spans across the step so both the top-of-step and
            // side-of-step voxels see the same solid/air profile, giving consistent
            // normals.  Fall back to ±1 when ±2 would reach outside the brick, and
            // skip the axis entirely when even ±1 crosses the brick boundary.
            ivec3 brickMax = ivec3(int(BrickSize));
            vec3 grad = vec3(0.0);
            for (int axis = 0; axis < 3; axis++) {
                ivec3 dn2 = ivec3(0); dn2[axis] = -2;
                ivec3 dp2 = ivec3(0); dp2[axis] =  2;
                ivec3 vn2 = voxel + dn2;
                ivec3 vp2 = voxel + dp2;
                bool in2N = all(greaterThanEqual(vn2, ivec3(0))) && all(lessThan(vn2, brickMax));
                bool in2P = all(greaterThanEqual(vp2, ivec3(0))) && all(lessThan(vp2, brickMax));
                if (in2N && in2P) {
                    float sn = texelFetch(brickPoolTexture, brickOriginPool + vn2, 0).r > 0u ? 1.0 : 0.0;
                    float sp = texelFetch(brickPoolTexture, brickOriginPool + vp2, 0).r > 0u ? 1.0 : 0.0;
                    grad[axis] = sn - sp;
                } else {
                    ivec3 dn1 = ivec3(0); dn1[axis] = -1;
                    ivec3 dp1 = ivec3(0); dp1[axis] =  1;
                    ivec3 vn1 = voxel + dn1;
                    ivec3 vp1 = voxel + dp1;
                    bool in1N = all(greaterThanEqual(vn1, ivec3(0))) && all(lessThan(vn1, brickMax));
                    bool in1P = all(greaterThanEqual(vp1, ivec3(0))) && all(lessThan(vp1, brickMax));
                    if (in1N && in1P) {
                        float sn = texelFetch(brickPoolTexture, brickOriginPool + vn1, 0).r > 0u ? 1.0 : 0.0;
                        float sp = texelFetch(brickPoolTexture, brickOriginPool + vp1, 0).r > 0u ? 1.0 : 0.0;
                        grad[axis] = sn - sp;
                    }
                    // else: skip axis — voxel is at brick boundary with no usable neighbour
                }
            }
            if (dot(grad, grad) > 1e-4) {
                hitNormal = normalize(grad);
                // Ensure the normal faces toward the camera (against the ray).
                if (dot(hitNormal, rd) > 0.0) hitNormal = -hitNormal;
            } else {
                // Zero gradient (interior voxel, brick-edge voxel with uniform
                // in-brick neighbourhood, or perfectly flat surface): fall back
                // to DDA entry-face normal.
                uint hitAxis = hasFaceMask ? resolveFaceAxis(faceMask, rd) : dominantAxis(rd);
                hitNormal = axisNormal(hitAxis, rd);
            }
            hitMaterialID = materialID;
            hitVoxelWorld = brickOriginWorld + uvec3(voxel);
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

    for (int stepIdx = 0; stepIdx < MaxMacroSteps; stepIdx++) {
        if (t >= tMax) {
            break;
        }

        uint currentNodeIdx = 0u;
        vec3 currentOrigin = vec3(0.0);
        float currentSize = sceneSize;

        for (int depth = 0; depth < MaxTraversalDepth; depth++) {
            SVONode currentNode = getSvoNode(currentNodeIdx);
            uint childMask = nodeChildMask(currentNode);

            if (currentNodeIdx == 0u && childMask == 0u && currentNode.childPointer == 0u && !isBrickLeaf(currentNode)) {
                return vec4(0.0, 0.0, 0.0, 0.0);
            }

            if (isBrickLeaf(currentNode)) {
                uint hitMaterialID = 0u;
                vec3 hitNormal = vec3(0.0);
                uvec3 hitVoxelWorld = uvec3(0u);
                uvec3 brickOrigin = unmirrorNodeOrigin(currentOrigin, currentSize, sceneSize, raySign);
                if (currentNode.childPointer == 0u) {
                    uint fallbackMaterialID = nodeMaterialID(currentNode);
                    if (fallbackMaterialID > 0u) {
                        // Use a fixed sky-facing normal for all fallback bricks.
                        // The SVO entry-face normal alternates between Z-face and side-faces
                        // on slopes, creating a herringbone stripe artifact.  A stable
                        // up-vector gives consistent base lighting; per-brick hash variation
                        // then breaks up the uniform flat appearance.
                        vec4 baseColor = shadeMaterial(fallbackMaterialID, vec3(0.0, 0.0, 1.0));
                        float bx = float(brickOrigin.x >> 3u);
                        float by = float(brickOrigin.y >> 3u);
                        float bz = float(brickOrigin.z >> 3u);
                        float h = fract(sin(dot(vec3(bx, by, bz), vec3(12.9898, 78.233, 37.719))) * 43758.5453);
                        float variation = 0.82 + h * 0.36;
                        return vec4(baseColor.rgb * variation, baseColor.a);
                    }
                    break;
                }
                if (raymarchBrick(currentNode.childPointer, brickOrigin, ro + rd * t, rd, faceMask, hasFaceMask, hitMaterialID, hitNormal, hitVoxelWorld)) {
                    float hvx = float(hitVoxelWorld.x);
                    float hvy = float(hitVoxelWorld.y);
                    float hvz = float(hitVoxelWorld.z);
                    // Two independent per-voxel hash values derived from world position.
                    // hv0/hv1 perturb the surface normal in the tangent plane (±~11°).
                    // Staircase voxels that share the same gradient-normal direction
                    // (the herringbone root cause) end up with randomised normals and
                    // therefore randomised lighting, breaking the visible stripe pattern.
                    // hv2 scales final brightness (±20 %) for additional texture grain.
                    float hv0 = fract(sin(dot(vec3(hvx,       hvy,       hvz),       vec3(12.9898, 78.233, 45.164))) * 43758.5453);
                    float hv1 = fract(sin(dot(vec3(hvx + 7.3, hvy - 3.1, hvz + 11.7), vec3(39.346, 11.135, 83.155))) * 43758.5453);
                    float hv2 = fract(sin(dot(vec3(hvx,       hvy,       hvz),        vec3(33.5,   67.1,   21.8)))   * 43758.5453);
                    float pu = (hv0 - 0.5) * 0.40;  // ±0.20, ~11° max tangential offset
                    float pv = (hv1 - 0.5) * 0.40;
                    vec3 refUp   = abs(hitNormal.z) < 0.9 ? vec3(0.0, 0.0, 1.0) : vec3(1.0, 0.0, 0.0);
                    vec3 tang    = normalize(cross(refUp, hitNormal));
                    vec3 bitang  = cross(hitNormal, tang);
                    vec3 pertNormal = normalize(hitNormal + tang * pu + bitang * pv);
                    vec4 color = shadeMaterial(hitMaterialID, pertNormal);
                    return vec4(color.rgb * (0.80 + hv2 * 0.40), color.a);
                }
                break;
            }

            if (isSolidLeaf(currentNode)) {
                // For solid leaves (no per-voxel data), use the entry-face normal.
                // When the ray hits from a side face (X/Y) but is travelling mostly
                // downward, it likely struck a slope transition rather than a true
                // vertical cliff — prefer Z-up to reduce coarse-scale herringbone.
                uint hitAxis;
                if (hasFaceMask) {
                    uint faceAxis = resolveFaceAxis(faceMask, rd);
                    bool sideHit = (faceAxis != AXIS_Z);
                    bool steepRay = abs(rd.z) > max(abs(rd.x), abs(rd.y));
                    hitAxis = (sideHit && steepRay) ? AXIS_Z : faceAxis;
                } else {
                    hitAxis = dominantAxis(rd);
                }
                vec4 slColor = shadeMaterial(nodeMaterialID(currentNode), axisNormal(hitAxis, rd));
                // Per-voxel hash for solid leaves: derive position from the ray hit point.
                // Match the ±20 % brightness range used for resident brick leaves.
                vec3 hitPos = ro + rd * t;
                float slx = floor(hitPos.x);
                float sly = floor(hitPos.y);
                float slz = floor(hitPos.z);
                float slh = fract(sin(dot(vec3(slx, sly, slz), vec3(12.9898, 78.233, 45.164))) * 43758.5453);
                return vec4(slColor.rgb * (0.80 + slh * 0.40), slColor.a);
            }

            if (currentNode.childPointer == 0u || childMask == 0u || currentSize <= 1.0) {
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

            currentOrigin += octantOffset(childSize, octant);
            currentSize = childSize;

            uint realOctant = octant ^ octantMask;
            if ((childMask & (1u << realOctant)) == 0u) {
                break;
            }

            uint maskBefore = childMask & ((1u << realOctant) - 1u);
            currentNodeIdx = currentNode.childPointer + bitCount(maskBefore);
        }

        vec3 exitTs = (currentOrigin + vec3(currentSize) - mirroredRo) * invDir;
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
