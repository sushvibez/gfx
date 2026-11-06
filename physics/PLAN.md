# physics — the framework's physics engine (plan)

**Status (2026-08): P0–P2 implemented** (`physics/p3`; per-phase notes
below). This is the roadmap for the Godot-parity physics track, written so
execution is a selection problem, not a research problem.

## Reference points

- **Godot** ships its own GodotPhysics (2D + 3D) and, since 4.4, defaults 3D
  to **Jolt Physics** (C++, MIT) — an admission that a game-quality 3D solver
  is its own discipline. Its API surface is what our users will expect:
  bodies (static/kinematic/rigid/area), shapes, ray/shape queries, collision
  layers/masks, `move_and_slide` character controllers, joints.
- **Jolt / Box2D** are the quality bars for 3D and 2D respectively. We are
  NOT porting either; we implement the subset our actual games need, pure Go,
  no cgo, deterministic enough for lockstep (replays).
- **Our consumers, in priority order:**
  1. **A first-person trainer** (Godot conversion target) — kinematic character
     movement, ray line-of-sight, hitscan, triggers, doors. Almost NO
     dynamics.
  2. **A 2D swarm game** — swarm movement, obstacle fields, projectiles.
  3. Future games — general rigid-body dynamics.

That priority order is the whole plan: queries → characters → dynamics.

## Architecture

- New module `physics/` with `physics/p3` (3D) and `physics/p2` (2D) sharing
  design but NOT code-generic (vector types differ; genericity costs clarity
  in the hot path).
- **World** owns bodies/shapes/broadphase; stepped at a **fixed timestep**
  (accumulator in the caller; `World.Step(dt)` pure). All state in flat
  slices with generation-handle ids (`mem` discipline, 0-alloc steps).
- **Integration with the engine:** ECS components (`Body`, `Collider`) sync
  to `scene.Tree` transforms after each step; debug-draw overlay through
  `Renderer3D` lines; js/wasm single-threaded (parallel islands are a
  scheduling policy, per the framework rule).
- **Determinism tiers:** T0 same-binary determinism (fixed timestep, ordered
  iteration, no map iteration in the step) → T1 cross-platform float
  determinism (careful math, no FMA divergence — validate wasm vs native in
  CI) → T2 fixed-point (only if lockstep multiplayer ever demands it).

## Phases

### P0 — Collision queries (static world)   ← first; the trainer's real need
*Status (2026-08): DONE — `p3/shape.go` (sphere/capsule/box/trimesh),
`p3/hull.go` (quickhull convex hull), `p3/world.go` (flat generation-handle
bodies + world BVH), `p3/query.go`/`capsule.go` (RayCast, ClosestPoint,
OverlapSphere/Capsule, CastSphere/Capsule), layers/masks on every query;
ray primitives promoted into `core/geom.go`, shared with render picking.
Deviations: the world BVH rebuilds on mutation instead of refitting (P0
worlds are near-static; revisit if profiling asks); triangle meshes are
two-sided surfaces with no inside, so rays from "inside" report the exit
face while solid shapes report t=0; convex hulls are query/static-only
(dynamic hulls wait on GJK/EPA, on P2's headroom list). Verified:
per-shape analytic oracles incl. parallel/grazing/inside rays, hull-vs-box
oracle fuzz, BVH-vs-brute-force ray fuzz, 0-alloc queries
(`TestQueryAllocs`), `BenchmarkRayCast10k` (10k rays/frame, ~1 µs/ray on a
400-body scene, 0 allocs).*
- Shapes: sphere, capsule, box (OBB), triangle mesh (static), convex hull.
- Static **BVH** over world geometry (build once, refit on edits).
- Queries: `RayCast` (we have ray/tri + AABB already in render/pick.go —
  promote the primitives into physics and share), `ShapeCast` (swept
  sphere/capsule), `Overlap` (shape vs world), closest-point.
- Collision layers/masks on every query.
- Verify: analytic oracles (known geometry), fuzz rays against a brute-force
  reference, bench 10k rays/frame.

### P1 — Kinematic character controller   ← makes the trainer walkable
*Status (2026-07): DONE — `p3/character.go` (MoveAndSlide with step-up,
slope limit, ground snap, ceiling check), triggers via
OverlapTriggers/TriggerEvents, kinematic platforms carrying the
controller; gauntlet scenes in `character_test.go`.*
- Capsule controller: `MoveAndSlide(vel, dt)` — sweep, slide on planes,
  step-up (stairs), slope limits, ground snapping, ceiling check.
- **Triggers/areas**: overlap volumes with enter/exit events.
- Moving platforms (kinematic bodies carrying the controller).
- Verify: corridor/stair/slope gauntlet scene replayed headlessly;
  golden trajectories.

### P2 — Rigid-body dynamics
*Status (2026-07): DONE (v1) — `p3/dynamics.go` / `contact.go` / `solver.go`.*
Shipped: semi-implicit Euler + implicit damping; specialized narrowphase
(sphere-vs-any, box-box SAT with reference-face clipping, box-vs-other by
corner sampling — no GJK/EPA yet, dynamic capsules deferred); sequential
impulses with positional warm starting; **split-impulse** position pass,
linear-only (angular position correction makes stacks "walk" sideways);
island-atomic sleeping via per-step union-find (a lone sleeper mid-pile is
an infinite-mass anvil the solver pumps energy against) with overlap-based
waking BEFORE narrowphase (waking after collect leaves the woken body
without its own support contacts for a step — energy pump).
Verified in `dynamics_test.go`: free-fall analytic, restitution apex,
friction slope stick/slide, 10-box tower 1000 steps (stands; top boxes keep
a bounded ≤0.6 lateral wander from residual solver jiggle), elastic
sphere-pair momentum, bit-exact determinism (2 runs × 300 steps × 17
bodies), sleep + impulse wake.
Deferred to headroom (revisit when a game needs it): block solver or
feature-ID persistent manifolds (kills the top-box wander, lets towers
sleep), GJK/EPA convex pairs, dynamic capsules, broadphase pair cache,
per-body CCD for bullets, energy-drift CI gate.

### P3 — Constraints & vehicles
- Joints: fixed, hinge (+limits/motor), slider, ball, 6-DOF (subset),
  distance/spring. Solved inside the same impulse loop.
- Ragdoll preset (capsule chain + cone limits) — pairs with skinning.
- Simple raycast vehicle (wheels as shapecasts) if a game needs it.

### P4 — 2D (`physics/p2`) for 2D games
- Same architecture, circle/box/polygon/segment shapes, spatial hash or
  AABB-tree broadphase; a swarm game's steering keeps its own flow-field
  logic and uses p2 for obstacles/projectiles/triggers.
- May land BEFORE P2/P3 if a 2D game needs it first — P0/P1 don't depend on
  the dynamics phases.

### P5 — Polish & tooling
- Debug draw (shapes, contacts, BVH heat), stats overlay.
- gml bindings: declare static colliders in scene .gml (per the
  gml-as-document-format decision); trigger handlers as gml signals.
- Broadphase/query profiling scene + CI perf gates.

## Explicit non-goals (revisit on demand)
Soft bodies, cloth, fluids, articulated robotics accuracy (Featherstone),
GPU physics, double-precision worlds, mid-phase BVH per convex mesh
(Jolt-style) until triangle-mesh dynamic bodies matter.

## Dependency notes
- P0 promotes/absorbs `render/pick.go`'s ray primitives (single source of
  intersection truth; picking then delegates to physics when both exist).
- Character controller consumes multi-touch/gamepad input work already done;
  animation (done: clips + skinning) pairs with P1 for animated characters.
- The trainer conversion can start on P0+P1 alone — schedule P2 by what the trainer
  actually needs, not speculatively.
