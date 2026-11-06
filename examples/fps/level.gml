// The fps walkthrough level as an executable scene document: solids get
// physics bodies automatically, and the door/platform positions BIND to
// host properties (doorOpen, platX) — when the game sets them, the physics
// bodies re-pose and the renderer follows, from this one declaration.
Scene3D {
    lightDir: [-0.45, -1, -0.3]

    // Ground and corridor walls (textures: scripts/fetch-assets.sh; they
    // soft-fall back to the flat colors until fetched).
    Box { size: [24, 0.5, 40]; position: [0, -0.25, 8]; color: "#40434d"
          texture: "../../build/textures/brick_pavement_02_diff_1k.jpg"; uvScale: 3 }
    Box { size: [0.6, 2.7, 40]; position: [-4, 1.35, 8]; color: "#6b7080"
          texture: "../../build/textures/damaged_plaster_diff_1k.jpg"; uvScale: 2.7 }
    Box { size: [0.6, 2.7, 40]; position: [4, 1.35, 8]; color: "#6b7080"
          texture: "../../build/textures/damaged_plaster_diff_1k.jpg"; uvScale: 2.7 }

    // Staircase up to a landing (auto-stepped by the character).
    Box { size: [3.2, 0.25, 0.8]; position: [-2, 0.125, 6.0]; color: "#597366"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }
    Box { size: [3.2, 0.50, 0.8]; position: [-2, 0.250, 6.8]; color: "#597366"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }
    Box { size: [3.2, 0.75, 0.8]; position: [-2, 0.375, 7.6]; color: "#597366"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }
    Box { size: [3.2, 1.00, 0.8]; position: [-2, 0.500, 8.4]; color: "#597366"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }
    Box { size: [3.2, 1.0, 5.2]; position: [-2, 0.5, 10.4]; color: "#597366"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }

    // 30° ramp back down on the other side.
    Box { size: [3, 0.24, 4.8]; position: [2, 0.52, 10.2]
          rotation: [1, 0, 0, 30]; color: "#73614d"
          texture: "../../build/textures/cracked_concrete_wall_diff_1k.jpg"; uvScale: 1.6 }

    // Oscillating platform (position bound to the host's platX).
    Box { id: mover; size: [2.4, 0.3, 2.4]; position: [platX, 0.9, 15.5]; color: "#4d7399"
          texture: "../../build/textures/metal_plate_diff_1k.jpg"; uvScale: 1.2 }

    // Pressure plate: trigger volume + a visible pad marker (render-only).
    Trigger { id: plate; size: [2.4, 1.2, 2.4]; position: [0, 0.6, 13] }
    Box { size: [2.4, 0.04, 2.4]; position: [0, 0.02, 13]; collider: false; color: "#40a659" }

    // Door: slides up as doorOpen goes 0→1.
    Box { id: door; size: [7.4, 2.2, 0.5]; position: [0, 1.1 + doorOpen * 2.3, 19]; color: "#994b47"
          texture: "../../build/textures/metal_plate_diff_1k.jpg"; uvScale: 1.2 }

    // An animated character on the landing (walks in place; soft-skipped
    // when the model isn't fetched — scripts/fetch-assets.sh).
    MeshInstance { source: "../../build/models/CesiumMan.glb"; animation: "0"
                   position: [-2, 1.0, 10.4]; rotationY: 180 }

    // A metallic orb landmark on the landing.
    Sphere { radius: 0.35; position: [-2, 1.35, 11.5]; color: "#3080ff"; metallic: 0.85; roughness: 0.3 }
    Cylinder { radius: 0.25; height: 2.6; position: [3.2, 1.3, 17.5]; color: "#806040" }
}
