#version 450

layout(location = 0) in vec2 uv;
layout(location = 1) in vec3 cameraPos;

layout(location = 0) out vec4 outColor;

const int steps = 16;

const int NX = 9;
const int NY = 9;
const int NZ = 9;
const int N = NX * NY * NZ;

int voxels[N] = int[](
// z = 0 (ground layer, all ground)
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
1,1,1, 1,1,1, 1,1,1,
// z = 1 (mostly empty, trunk base at center)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 2 (trunk)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 3 (trunk continues)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,2,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 4 (lower canopy — 5x5 block centered)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 5 (middle canopy — 3x3)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,3,0, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 3,3,3, 0,0,0,
0,0,0, 0,3,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 6 (top leaf)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,3,0, 0,0,0,
0,0,0, 0,3,0, 0,0,0,
0,0,0, 0,3,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 7 (empty)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
// z = 8 (empty)
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0,
0,0,0, 0,0,0, 0,0,0
);


int voxelAt(ivec3 p) {
    if (p.x < 0 || p.y < 0 || p.z < 0 ||
        p.x >= NX || p.y >= NY || p.z >= NZ) return 0;
    int idx = p.x + p.y * NX + p.z * NX * NY;
    return voxels[idx];
}

vec4 raymarchVoxels(vec3 ro, vec3 rd) {
    // Current voxel coordinate
    ivec3 mapPos = ivec3(floor(ro));
    
    // Ray delta distances
    vec3 deltaDist = abs(1.0 / rd);
    
    // Step direction and initial side distances
    ivec3 stepDir;
    vec3 sideDist;
    
    // Setup DDA parameters for X, Y, and Z axes
    if (rd.x < 0.0) {
        stepDir.x = -1;
        sideDist.x = (ro.x - float(mapPos.x)) * deltaDist.x;
    } else {
        stepDir.x = 1;
        sideDist.x = (float(mapPos.x + 1) - ro.x) * deltaDist.x;
    }
    
    if (rd.y < 0.0) {
        stepDir.y = -1;
        sideDist.y = (ro.y - float(mapPos.y)) * deltaDist.y;
    } else {
        stepDir.y = 1;
        sideDist.y = (float(mapPos.y + 1) - ro.y) * deltaDist.y;
    }
    
    if (rd.z < 0.0) {
        stepDir.z = -1;
        sideDist.z = (ro.z - float(mapPos.z)) * deltaDist.z;
    } else {
        stepDir.z = 1;
        sideDist.z = (float(mapPos.z + 1) - ro.z) * deltaDist.z;
    }

    // Main Traversal Loop
    const int MAX_STEPS = 64; 
    int hitVoxelType = 0;
    vec3 normal = vec3(0.0);

    for (int i = 0; i < MAX_STEPS; i++) {
        // Read the voxel array at the current location
        int idx = mapPos.x + (mapPos.y * NX) + (mapPos.z * NX * NY);
        
        // Out of bounds break
        if (mapPos.x < 0 || mapPos.x >= NX || 
            mapPos.y < 0 || mapPos.y >= NY || 
            mapPos.z < 0 || mapPos.z >= NZ) break;
            
        if (voxels[idx] > 0) {
            hitVoxelType = voxels[idx];
            break; // Found solid voxel!
        }
        
        // Jump to the next nearest grid boundary (DDA core step)
        if (sideDist.x < sideDist.y) {
            if (sideDist.x < sideDist.z) {
                sideDist.x += deltaDist.x;
                mapPos.x += stepDir.x;
                normal = vec3(-float(stepDir.x), 0.0, 0.0);
            } else {
                sideDist.z += deltaDist.z;
                mapPos.z += stepDir.z;
                normal = vec3(0.0, 0.0, -float(stepDir.z));
            }
        } else {
            if (sideDist.y < sideDist.z) {
                sideDist.y += deltaDist.y;
                mapPos.y += stepDir.y;
                normal = vec3(0.0, -float(stepDir.y), 0.0);
            } else {
                sideDist.z += deltaDist.z;
                mapPos.z += stepDir.z;
                normal = vec3(0.0, 0.0, -float(stepDir.z));
            }
        }
    }

    // Shading based on hit voxel type and normal
    if (hitVoxelType > 0) {
        float lighting = dot(normal, normalize(vec3(0.5, 1.0, 0.3))) * 0.5 + 0.5;
        return vec4(vec3(0.2, 0.6, 1.0) * lighting, 1.0); // Output voxel color
    }
    
    return vec4(0.0); // Sky color (miss)
}

void main () {    
    vec3 camForward = vec3(0.0, 0.0, -1.0); 
    vec3 camRight   = vec3(1.0, 0.0,  0.0);
    vec3 camUp      = vec3(0.0, 1.0,  0.0);
    
    vec3 rayDir = normalize(camForward + camRight * uv.x + camUp * uv.y);
    outColor = raymarchVoxels(cameraPos, rayDir);
}