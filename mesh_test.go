package gfx

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// meshTestBlocks is a small palette used by the mesh tests.
var (
	meshTestRed  = BlockColor{Light: ColorNRGBA(255, 0, 0, 255), Medium: ColorNRGBA(200, 0, 0, 255), Dark: ColorNRGBA(100, 0, 0, 255)}
	meshTestBlue = BlockColor{Light: ColorNRGBA(0, 0, 255, 255), Medium: ColorNRGBA(0, 0, 200, 255), Dark: ColorNRGBA(0, 0, 100, 255)}
)

func mustMesh(t *testing.T, blocks Blocks, options ...MeshOption) *Mesh {
	t.Helper()

	m, err := blocks.Mesh(options...)
	if err != nil {
		t.Fatalf("Blocks.Mesh() error = %v", err)
	}

	return m
}

// meshFaceArea returns the area of face f.
func meshFaceArea(m *Mesh, f MeshFace) float64 {
	a := m.Positions[f.Vertices[0]]
	b := m.Positions[f.Vertices[1]]
	c := m.Positions[f.Vertices[2]]
	d := m.Positions[f.Vertices[3]]

	return meshTriangleArea(a, b, c) + meshTriangleArea(a, c, d)
}

// meshFaceRect returns the integer bounds of face f.
func meshFaceRect(m *Mesh, f MeshFace) (lo, hi [3]int64) {
	p := m.Positions[f.Vertices[0]]

	lo = [3]int64{int64(p.X), int64(p.Y), int64(p.Z)}
	hi = lo

	for _, v := range f.Vertices[1:] {
		q := m.Positions[v]
		r := [3]int64{int64(q.X), int64(q.Y), int64(q.Z)}

		for a := range 3 {
			lo[a] = min(lo[a], r[a])
			hi[a] = max(hi[a], r[a])
		}
	}

	return lo, hi
}

func TestBlocksMeshTwoBlocks(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestBlue),
	}

	m := mustMesh(t, blocks)

	// Two unit cubes sharing a face: 12 faces minus the 2 touching faces.
	if got, want := len(m.Faces), 10; got != want {
		t.Fatalf("len(m.Faces) = %d, want %d", got, want)
	}

	if got, want := len(m.Positions), 4*len(m.Faces); got != want {
		t.Fatalf("len(m.Positions) = %d, want %d", got, want)
	}

	if got, want := len(m.Triangles), 2*len(m.Faces); got != want {
		t.Fatalf("len(m.Triangles) = %d, want %d", got, want)
	}

	if got, want := m.Area(), 10.0; got != want {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}

	// The touching planes at x=0 and x=2 may only have outward faces;
	// the shared plane at x=1 must have no faces at all.
	for i, f := range m.Faces {
		n := m.Normals[f.Normal]
		lo, hi := meshFaceRect(m, f)

		if lo[0] == 1 && hi[0] == 1 {
			t.Fatalf("face %d lies on the shared plane at x=1 (normal %v)", i, n)
		}
	}
}

func TestBlocksMeshStaircase(t *testing.T) {
	// A three step staircase along X, each step one unit deeper in Z.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 2), meshTestRed),
		NewBlock(V3(2, 0, 0), V3(1, 1, 3), meshTestRed),
	}

	m := mustMesh(t, blocks)

	// Exposed area:
	//   tops:    1 + 2 + 3 = 6
	//   bottoms: 1 + 2 + 3 = 6
	//   +X ends: 1 + 2 + 3 = 6 (the step ends face each other? no)
	//   -X ends: 1 (only the first)
	//   +Y, -Y per block: 1+2+3, twice = 12
	//   The +X face of a step is covered only where the next step touches.
	//
	// Instead of hand-summing all of that, verify total exposed area
	// against an independent face-by-face computation.
	if got, want := m.Area(), meshBruteForceArea(blocks); got != want {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}

	// No leftover geometry: no two faces of opposite normal may occupy
	// the same plane rectangle.
	for i, f := range m.Faces {
		for j := i + 1; j < len(m.Faces); j++ {
			g := m.Faces[j]

			if !m.Normals[f.Normal].Eq(m.Normals[g.Normal].Scaled(-1)) {
				continue
			}

			if meshFacesCoincide(m, f, g) {
				t.Fatalf("faces %d and %d coincide", i, j)
			}
		}
	}
}

// meshFacesCoincide reports whether the two faces exactly cover the same
// axis-aligned area.
func meshFacesCoincide(m *Mesh, a, b MeshFace) bool {
	amin, amax := meshFaceRect(m, a)
	bmin, bmax := meshFaceRect(m, b)

	return amin == bmin && amax == bmax
}

// meshBruteForceArea computes the exposed surface area by testing which
// unit cells of each block face are covered by another block. It only
// supports unit-sized cells (all test blocks use unit cells) and is used
// as an independent reference.
func meshBruteForceArea(blocks Blocks) float64 {
	type cell struct{ x, y, z int64 }

	occupied := map[cell]bool{}

	for _, b := range blocks {
		for x := int64(b.Pos.X); x < int64(b.Pos.X+b.Size.X); x++ {
			for y := int64(b.Pos.Y); y < int64(b.Pos.Y+b.Size.Y); y++ {
				for z := int64(b.Pos.Z); z < int64(b.Pos.Z+b.Size.Z); z++ {
					occupied[cell{x, y, z}] = true
				}
			}
		}
	}

	var area float64

	dirs := [][3]int64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}

	for c := range occupied {
		for _, d := range dirs {
			if !occupied[cell{c.x + d[0], c.y + d[1], c.z + d[2]}] {
				area++
			}
		}
	}

	return area
}

func TestBlocksMeshDifferentSizesAndNegativeCoordinates(t *testing.T) {
	// Blocks with different integer sizes and negative coordinates.
	blocks := Blocks{
		NewBlock(V3(-5, -4, -3), V3(2, 3, 4), meshTestRed),
		NewBlock(V3(-3, -1, 1), V3(1, 2, 2), meshTestBlue),
	}

	m := mustMesh(t, blocks)

	// The blue block is fully on one side of the red block (x=-3 is its
	// touching face) and touches along its -X face.
	if got, want := m.Area(), meshExpectedArea(blocks); got != want {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}

	for i, f := range m.Faces {
		p := m.Positions[f.Vertices[0]]

		if p.X < -5 || p.Y < -4 || p.Z < -3 || p.X > -2 || p.Y > 1 || p.Z > 3 {
			t.Fatalf("face %d position %v outside the blocks", i, p)
		}
	}
}

// meshExpectedArea computes the exposed area per block face, without any
// clipping, for blocks that only touch along full faces.
func meshExpectedArea(blocks Blocks) float64 {
	var total float64

	for i, b := range blocks {
		for axis := range 3 {
			for _, side := range []int{-1, 1} {
				covered := false

				for j, other := range blocks {
					if i != j && meshBlocksFullyCover(b, other, axis, side) {
						covered = true
						break
					}
				}

				if !covered {
					size := [3]float64{b.Size.X, b.Size.Y, b.Size.Z}
					total += size[(axis+1)%3] * size[(axis+2)%3]
				}
			}
		}
	}

	return total
}

// meshBlocksFullyCover reports whether other fully covers the given face
// of b (assuming the blocks are aligned and touching).
func meshBlocksFullyCover(b, other Block, axis, side int) bool {
	bmin := [3]float64{b.Pos.X, b.Pos.Y, b.Pos.Z}
	bmax := [3]float64{b.Pos.X + b.Size.X, b.Pos.Y + b.Size.Y, b.Pos.Z + b.Size.Z}
	omin := [3]float64{other.Pos.X, other.Pos.Y, other.Pos.Z}
	omax := [3]float64{other.Pos.X + other.Size.X, other.Pos.Y + other.Size.Y, other.Pos.Z + other.Size.Z}

	if side > 0 {
		if omin[axis] != bmax[axis] {
			return false
		}
	} else if omax[axis] != bmin[axis] {
		return false
	}

	for a := range 3 {
		if a != axis && (omin[a] > bmin[a] || omax[a] < bmax[a]) {
			return false
		}
	}

	return true
}

func TestBlocksMeshNormals(t *testing.T) {
	m := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(2, 3, 4), meshTestRed),
	})

	// A single block has six outward normals, one per face.
	if got, want := len(m.Normals), 6; got != want {
		t.Fatalf("len(m.Normals) = %d, want %d", got, want)
	}

	seen := map[Vec3]bool{}

	for _, f := range m.Faces {
		n := m.Normals[f.Normal]
		seen[n] = true

		if got := n.Len(); math.Abs(got-1) > 1e-12 {
			t.Fatalf("normal %v length = %v, want 1", n, got)
		}

		// The cross product of the first two edges must point the same
		// way as the normal.
		a := m.Positions[f.Vertices[0]]
		b := m.Positions[f.Vertices[1]]
		c := m.Positions[f.Vertices[2]]

		if got := meshCross(b.Sub(a), c.Sub(a)).Dot(n); got <= 0 {
			t.Fatalf("face winding not in the direction of normal %v", n)
		}

		// Every triangle must use the normal of its face.
		for _, tri := range m.Triangles {
			if m.Faces[tri.Face].Vertices == f.Vertices && tri.Normal != f.Normal {
				t.Fatalf("triangle normal %d != face normal %d", tri.Normal, f.Normal)
			}
		}
	}

	for _, n := range []Vec3{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	} {
		if !seen[n] {
			t.Fatalf("normal %v not used by any face", n)
		}
	}

	if got, want := m.Area(), 2*(2*3+2*4+3*4); got != float64(want) {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}
}

func TestBlocksMeshColors(t *testing.T) {
	m := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
	})

	for _, f := range m.Faces {
		n := m.Normals[f.Normal]
		c := m.Materials[f.Material].Color

		if c != f.Color {
			t.Fatalf("face color %v != material color %v", f.Color, c)
		}

		switch {
		case n.Eq(V3(0, 0, 1)) && c != meshTestRed.Light:
			t.Fatalf("top color = %v, want %v", c, meshTestRed.Light)
		case n.Eq(V3(0, 0, -1)) && c != meshTestRed.Dark:
			t.Fatalf("bottom color = %v, want %v", c, meshTestRed.Dark)
		case n.Eq(V3(-1, 0, 0)) && c != meshTestRed.Dark:
			t.Fatalf("left color = %v, want %v", c, meshTestRed.Dark)
		case (n.Eq(V3(1, 0, 0)) || n.Eq(V3(0, 1, 0)) || n.Eq(V3(0, -1, 0))) && c != meshTestRed.Medium:
			t.Fatalf("side color = %v, want %v", c, meshTestRed.Medium)
		}
	}

	// A single block can use at most three materials.
	if got, want := len(m.Materials), 3; got != want {
		t.Fatalf("len(m.Materials) = %d, want %d", got, want)
	}

	// Two different colors create more distinct materials.
	m2 := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(2, 0, 0), V3(1, 1, 1), meshTestBlue),
	})

	if got, want := len(m2.Materials), 6; got != want {
		t.Fatalf("len(m2.Materials) = %d, want %d", got, want)
	}
}

func TestBlocksMeshDeterministic(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(2, 1, 1), meshTestRed),
		NewBlock(V3(2, 0, 0), V3(1, 2, 1), meshTestBlue),
		NewBlock(V3(2, 2, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(0, 1, 0), V3(1, 1, 1), meshTestBlue),
	}

	want := mustMesh(t, blocks)

	for _, perm := range [][]int{
		{3, 2, 1, 0},
		{1, 0, 3, 2},
		{2, 3, 0, 1},
	} {
		permuted := make(Blocks, len(blocks))
		for i, j := range perm {
			permuted[i] = blocks[j]
		}

		got := mustMesh(t, permuted)

		if !meshEqual(got, want) {
			t.Fatalf("mesh for permutation %v differs", perm)
		}

		var a, b bytes.Buffer

		if err := got.WriteOBJ(&a, "m.mtl"); err != nil {
			t.Fatal(err)
		}

		if err := want.WriteOBJ(&b, "m.mtl"); err != nil {
			t.Fatal(err)
		}

		if a.String() != b.String() {
			t.Fatalf("OBJ for permutation %v differs", perm)
		}
	}
}

func meshEqual(a, b *Mesh) bool {
	if len(a.Positions) != len(b.Positions) ||
		len(a.Normals) != len(b.Normals) ||
		len(a.Faces) != len(b.Faces) ||
		len(a.Triangles) != len(b.Triangles) ||
		len(a.Materials) != len(b.Materials) {
		return false
	}

	for i := range a.Positions {
		if a.Positions[i] != b.Positions[i] {
			return false
		}
	}

	for i := range a.Faces {
		if a.Faces[i] != b.Faces[i] {
			return false
		}
	}

	for i := range a.Triangles {
		if a.Triangles[i] != b.Triangles[i] {
			return false
		}
	}

	for i := range a.Materials {
		if a.Materials[i] != b.Materials[i] {
			return false
		}
	}

	return true
}

func TestBlocksMeshMerge(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestRed),
	}

	unmerged := mustMesh(t, blocks)
	merged := mustMesh(t, blocks, WithMeshMerge())

	if got, want := len(merged.Faces), 6; got != want {
		t.Fatalf("len(merged.Faces) = %d, want %d", got, want)
	}

	if got, want := merged.Area(), unmerged.Area(); math.Abs(got-want) > 1e-12 {
		t.Fatalf("merged area = %v, want %v", got, want)
	}

	for _, f := range merged.Faces {
		lo, hi := meshFaceRect(merged, f)

		if n := merged.Normals[f.Normal]; !n.Eq(V3(-1, 0, 0)) && !n.Eq(V3(1, 0, 0)) {
			// Faces spanning X become 2x1 quads.
			if got, want := hi[0]-lo[0], int64(2); got != want {
				t.Fatalf("merged face X extent = %d, want %d", got, want)
			}
		}
	}
}

func TestBlocksMeshMergeColorBarrier(t *testing.T) {
	// Adjacent same-plane faces of different colors must not merge.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestBlue),
	}

	m := mustMesh(t, blocks, WithMeshMerge())

	// Still 10 faces: five per block, none mergeable with the other color.
	if got, want := len(m.Faces), 10; got != want {
		t.Fatalf("len(m.Faces) = %d, want %d", got, want)
	}

	for _, f := range m.Faces {
		if m.Normals[f.Normal].Eq(V3(0, 0, 1)) && meshFaceArea(m, f) != 1 {
			t.Fatalf("top face area = %v, want 1", meshFaceArea(m, f))
		}
	}
}

func TestBlocksMeshMergeDeterministic(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(2, 0, 0), V3(1, 1, 2), meshTestRed),
		NewBlock(V3(0, 1, 0), V3(3, 1, 1), meshTestRed),
	}

	want := mustMesh(t, blocks, WithMeshMerge())

	for _, perm := range [][]int{{3, 1, 0, 2}, {2, 3, 0, 1}} {
		permuted := make(Blocks, len(blocks))
		for i, j := range perm {
			permuted[i] = blocks[j]
		}

		if got := mustMesh(t, permuted, WithMeshMerge()); !meshEqual(got, want) {
			t.Fatalf("merged mesh for permutation %v differs", perm)
		}
	}
}

func TestBlocksMeshPartialCoverage(t *testing.T) {
	// A tall block next to a short one: the tall block's +X face is only
	// partially covered and must keep its exposed part.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 2), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestBlue),
	}

	m := mustMesh(t, blocks)

	// Exposed area is verified against the cell-based reference.
	if got, want := m.Area(), meshCellAreaFilled(blocks); got != want {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}

	// No +X face vertex of the tall block may lie below z=1, where the
	// short block hides it.
	for i, f := range m.Faces {
		if !m.Normals[f.Normal].Eq(V3(1, 0, 0)) {
			continue
		}

		lo, _ := meshFaceRect(m, f)

		if lo[0] == 1 && lo[2] < 1 {
			t.Fatalf("face %d has a hidden +X part at z=%d", i, lo[2])
		}
	}
}

// TestBlocksMeshPartialCoverageOffset covers a face with a block that only
// covers part of it on both axes, so the exposed part is not a single
// simple strip.
func TestBlocksMeshPartialCoverageOffset(t *testing.T) {
	// The middle block's +X face (a 3x3 square) is covered by two blocks
	// whose combined cover leaves an L-shaped exposed region.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 3, 3), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 2, 2), meshTestBlue),
		NewBlock(V3(1, 2, 0), V3(1, 1, 1), meshTestBlue),
	}

	m := mustMesh(t, blocks)

	if got, want := m.Area(), meshCellAreaFilled(blocks); got != want {
		t.Fatalf("m.Area() = %v, want %v", got, want)
	}

	// The exposed +X area of the red block is 3x3 minus the 2x2 and 1x1
	// covers = 4, and must all be present in the mesh.
	var exposed float64

	for _, f := range m.Faces {
		if !m.Normals[f.Normal].Eq(V3(1, 0, 0)) {
			continue
		}

		lo, hi := meshFaceRect(m, f)

		if lo[0] != 1 || hi[0] != 1 {
			continue
		}

		exposed += meshFaceArea(m, f)
	}

	if got, want := exposed, 4.0; got != want {
		t.Fatalf("exposed +X area = %v, want %v", got, want)
	}
}

// meshCellAreaFilled returns the exposed area of the blocks based on unit
// cells; it is only valid for blocks with integer coordinates.
func meshCellAreaFilled(blocks Blocks) float64 {
	occupied := map[[3]int64]bool{}

	for _, b := range blocks {
		for x := int64(b.Pos.X); x < int64(b.Pos.X+b.Size.X); x++ {
			for y := int64(b.Pos.Y); y < int64(b.Pos.Y+b.Size.Y); y++ {
				for z := int64(b.Pos.Z); z < int64(b.Pos.Z+b.Size.Z); z++ {
					occupied[[3]int64{x, y, z}] = true
				}
			}
		}
	}

	return meshCellArea(occupied)
}

func TestBlocksMeshErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks Blocks
		want   error
	}{
		{
			name:   "NaN position",
			blocks: Blocks{NewBlock(V3(math.NaN(), 0, 0), V3(1, 1, 1), meshTestRed)},
			want:   ErrMeshNonFinite,
		},
		{
			name:   "infinite size",
			blocks: Blocks{NewBlock(V3(0, 0, 0), V3(1, 1, math.Inf(1)), meshTestRed)},
			want:   ErrMeshNonFinite,
		},
		{
			name:   "negative infinity position",
			blocks: Blocks{NewBlock(V3(0, math.Inf(-1), 0), V3(1, 1, 1), meshTestRed)},
			want:   ErrMeshNonFinite,
		},
		{
			name:   "fractional position",
			blocks: Blocks{NewBlock(V3(0.5, 0, 0), V3(1, 1, 1), meshTestRed)},
			want:   ErrMeshNotGridAligned,
		},
		{
			name:   "fractional size",
			blocks: Blocks{NewBlock(V3(0, 0, 0), V3(1, 1.5, 1), meshTestRed)},
			want:   ErrMeshNotGridAligned,
		},
		{
			name:   "zero size",
			blocks: Blocks{NewBlock(V3(0, 0, 0), V3(0, 1, 1), meshTestRed)},
			want:   ErrMeshDegenerateBlock,
		},
		{
			name:   "negative size",
			blocks: Blocks{NewBlock(V3(0, 0, 0), V3(1, -1, 1), meshTestRed)},
			want:   ErrMeshDegenerateBlock,
		},
		{
			name:   "coordinate range",
			blocks: Blocks{NewBlock(V3(0, 0, 0), V3(math.Ldexp(1, 60), 1, 1), meshTestRed)},
			want:   ErrMeshCoordinateRange,
		},
		{
			name: "partial overlap",
			blocks: Blocks{
				NewBlock(V3(0, 0, 0), V3(2, 2, 2), meshTestRed),
				NewBlock(V3(1, 1, 1), V3(2, 2, 2), meshTestBlue),
			},
			want: ErrMeshOverlap,
		},
		{
			name: "contained overlap",
			blocks: Blocks{
				NewBlock(V3(0, 0, 0), V3(3, 3, 3), meshTestRed),
				NewBlock(V3(1, 1, 1), V3(1, 1, 1), meshTestBlue),
			},
			want: ErrMeshOverlap,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.blocks.Mesh(); !errors.Is(err, tc.want) {
				t.Fatalf("Blocks.Mesh() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBlocksMeshTouchingIsNotOverlap(t *testing.T) {
	for _, blocks := range []Blocks{
		{ // face
			NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
			NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestBlue),
		},
		{ // edge
			NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
			NewBlock(V3(1, 1, 0), V3(1, 1, 1), meshTestBlue),
		},
		{ // corner
			NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
			NewBlock(V3(1, 1, 1), V3(1, 1, 1), meshTestBlue),
		},
	} {
		if _, err := blocks.Mesh(); err != nil {
			t.Fatalf("Blocks.Mesh() error = %v", err)
		}
	}
}

func TestBlocksMeshFaceLimit(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestRed),
	}

	if _, err := blocks.Mesh(WithMeshMaxFaces(5)); !errors.Is(err, ErrMeshFaceLimit) {
		t.Fatalf("Blocks.Mesh() error = %v, want %v", err, ErrMeshFaceLimit)
	}

	if _, err := blocks.Mesh(WithMeshMaxFaces(10)); err != nil {
		t.Fatalf("Blocks.Mesh() error = %v, want nil", err)
	}

	// Merging happens before the limit is applied.
	if _, err := blocks.Mesh(WithMeshMerge(), WithMeshMaxFaces(6)); err != nil {
		t.Fatalf("Blocks.Mesh() error = %v, want nil", err)
	}
}

func TestMeshWriteOBJ(t *testing.T) {
	m := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
	})

	var buf bytes.Buffer

	if err := m.WriteMTL(&buf); err != nil {
		t.Fatalf("WriteMTL() error = %v", err)
	}

	mtl := buf.String()

	if !strings.Contains(mtl, "newmtl material_0") {
		t.Fatalf("MTL does not contain a material:\n%s", mtl)
	}

	if !strings.Contains(mtl, "Kd 1 0 0") {
		t.Fatalf("MTL does not contain the light color:\n%s", mtl)
	}

	buf.Reset()

	if err := m.WriteOBJ(&buf, "m.mtl"); err != nil {
		t.Fatalf("WriteOBJ() error = %v", err)
	}

	obj := buf.String()

	if !strings.HasPrefix(obj, "# gfx block mesh\n") {
		t.Fatalf("OBJ header = %q", strings.SplitN(obj, "\n", 2)[0])
	}

	if !strings.Contains(obj, "mtllib m.mtl\n") {
		t.Fatalf("OBJ does not reference the MTL:\n%s", obj)
	}

	if got, want := strings.Count(obj, "\nv "), 24; got != want {
		t.Fatalf("OBJ vertex lines = %d, want %d", got, want)
	}

	if got, want := strings.Count(obj, "\nvn "), 6; got != want {
		t.Fatalf("OBJ normal lines = %d, want %d", got, want)
	}

	if got, want := strings.Count(obj, "\nf "), 12; got != want {
		t.Fatalf("OBJ face lines = %d, want %d", got, want)
	}

	if !strings.Contains(obj, "\nf 1//") {
		t.Fatalf("OBJ face indices are not 1-based:\n%s", obj)
	}
}

func TestMeshWriteOBJNoMTL(t *testing.T) {
	m := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
	})

	var buf bytes.Buffer

	if err := m.WriteOBJ(&buf, ""); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), "mtllib") {
		t.Fatalf("OBJ contains mtllib even though none was requested:\n%s", buf.String())
	}
}

func TestMeshSaveOBJ(t *testing.T) {
	dir := t.TempDir()
	objPath := filepath.Join(dir, "mesh.obj")
	mtlPath := filepath.Join(dir, "mesh.mtl")

	m := mustMesh(t, Blocks{
		NewBlock(V3(-1, -1, -1), V3(2, 2, 2), meshTestRed),
	})

	if err := m.SaveOBJ(objPath, mtlPath); err != nil {
		t.Fatalf("SaveOBJ() error = %v", err)
	}

	obj, err := os.ReadFile(objPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(obj), "mtllib mesh.mtl") {
		t.Fatalf("OBJ does not reference the MTL by base name:\n%s", obj)
	}

	if _, err := os.Stat(mtlPath); err != nil {
		t.Fatalf("MTL was not written: %v", err)
	}
}

func TestMeshOBJReadBack(t *testing.T) {
	m := mustMesh(t, Blocks{
		NewBlock(V3(0, 0, 0), V3(2, 1, 1), meshTestRed),
		NewBlock(V3(2, 0, 0), V3(1, 2, 1), meshTestBlue),
	})

	var buf bytes.Buffer

	if err := m.WriteOBJ(&buf, ""); err != nil {
		t.Fatal(err)
	}

	obj := parseOBJ(t, buf.String())

	if got, want := len(obj.vertices), len(m.Positions); got != want {
		t.Fatalf("OBJ vertices = %d, want %d", got, want)
	}

	if got, want := len(obj.normals), len(m.Normals); got != want {
		t.Fatalf("OBJ normals = %d, want %d", got, want)
	}

	if got, want := len(obj.faces), len(m.Triangles); got != want {
		t.Fatalf("OBJ triangles = %d, want %d", got, want)
	}

	var area float64

	for _, f := range obj.faces {
		var tri [3]Vec3

		for i, v := range f {
			if v.v < 1 || v.v > len(obj.vertices) {
				t.Fatalf("vertex index %d out of range", v.v)
			}

			if v.n < 1 || v.n > len(obj.normals) {
				t.Fatalf("normal index %d out of range", v.n)
			}

			tri[i] = obj.vertices[v.v-1]
		}

		// The winding must agree with the stored normal.
		n := obj.normals[f[0].n-1]

		if cross := meshCross(tri[1].Sub(tri[0]), tri[2].Sub(tri[0])); cross.Dot(n) <= 0 {
			t.Fatalf("OBJ triangle winding disagrees with normal %v", n)
		}

		area += meshTriangleArea(tri[0], tri[1], tri[2])
	}

	if got, want := area, m.Area(); math.Abs(got-want) > 1e-12 {
		t.Fatalf("OBJ area = %v, want %v", got, want)
	}
}

func TestMeshWriteOBJInvalid(t *testing.T) {
	m := &Mesh{
		Positions: []Vec3{{0, 0, 0}},
		Faces: []MeshFace{{
			Vertices: [4]int{0, 1, 2, 3},
		}},
	}

	if err := m.WriteOBJ(&bytes.Buffer{}, ""); !errors.Is(err, ErrMeshInvalid) {
		t.Fatalf("WriteOBJ() error = %v, want %v", err, ErrMeshInvalid)
	}

	if err := m.WriteMTL(&bytes.Buffer{}); !errors.Is(err, ErrMeshInvalid) {
		t.Fatalf("WriteMTL() error = %v, want %v", err, ErrMeshInvalid)
	}

	var nilMesh *Mesh

	if err := nilMesh.WriteOBJ(&bytes.Buffer{}, ""); !errors.Is(err, ErrMeshInvalid) {
		t.Fatalf("nil WriteOBJ() error = %v, want %v", err, ErrMeshInvalid)
	}
}

func TestBlocksMeshEmpty(t *testing.T) {
	m := mustMesh(t, nil)

	if got := len(m.Faces); got != 0 {
		t.Fatalf("len(m.Faces) = %d, want 0", got)
	}

	if got := m.Area(); got != 0 {
		t.Fatalf("m.Area() = %v, want 0", got)
	}

	var buf bytes.Buffer

	if err := m.WriteOBJ(&buf, "m.mtl"); err != nil {
		t.Fatalf("WriteOBJ() error = %v", err)
	}
}

// TestBlocksMeshDrawRegression checks that adding mesh export did not
// change the original isometric drawing of blocks.
func TestBlocksMeshDrawRegression(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(10, 10, 10), meshTestRed),
		NewBlock(V3(10, 0, 0), V3(10, 10, 10), meshTestBlue),
	}

	origin := V3(80, 100, 1)

	before := NewImage(160, 120)
	blocks.Draw(before, origin)

	// Building a mesh must not mutate the blocks or their drawing.
	if _, err := blocks.Mesh(WithMeshMerge()); err != nil {
		t.Fatal(err)
	}

	after := NewImage(160, 120)
	blocks.Draw(after, origin)

	if !bytes.Equal(before.Pix, after.Pix) {
		t.Fatalf("Blocks.Draw output changed after calling Blocks.Mesh")
	}
}

// --- a very small OBJ reader used to verify WriteOBJ ---

type meshOBJVertex struct {
	v, n int
}

type meshReadOBJ struct {
	vertices  []Vec3
	normals   []Vec3
	faces     [][3]meshOBJVertex
	materials []string
}

func parseOBJ(t *testing.T, s string) meshReadOBJ {
	t.Helper()

	var out meshReadOBJ

	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "v":
			if len(fields) != 4 {
				t.Fatalf("bad vertex line %q", line)
			}

			out.vertices = append(out.vertices, V3(
				parseFloat(t, fields[1]),
				parseFloat(t, fields[2]),
				parseFloat(t, fields[3])))
		case "vn":
			if len(fields) != 4 {
				t.Fatalf("bad normal line %q", line)
			}

			out.normals = append(out.normals, V3(
				parseFloat(t, fields[1]),
				parseFloat(t, fields[2]),
				parseFloat(t, fields[3])))
		case "usemtl":
			if len(fields) != 2 {
				t.Fatalf("bad usemtl line %q", line)
			}

			out.materials = append(out.materials, fields[1])
		case "f":
			if len(fields) != 4 {
				t.Fatalf("bad face line %q", line)
			}

			var tri [3]meshOBJVertex

			for i, f := range fields[1:] {
				parts := strings.Split(f, "//")
				if len(parts) != 2 {
					t.Fatalf("bad face vertex %q", f)
				}

				tri[i] = meshOBJVertex{
					v: parseInt(t, parts[0]),
					n: parseInt(t, parts[1]),
				}
			}

			out.faces = append(out.faces, tri)
		}
	}

	return out
}

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("ParseFloat(%q): %v", s, err)
	}

	return v
}

func parseInt(t *testing.T, s string) int {
	t.Helper()

	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", s, err)
	}

	return n
}

// TestBlocksMeshRandomDifferential compares the mesh area against a
// unit-cell reference over many random non-overlapping block layouts,
// with and without merging, and checks that merging never changes the
// total area.
func TestBlocksMeshRandomDifferential(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))

	occupied := map[[3]int64]bool{}

	var blocks Blocks

	for range 40 {
		size := [3]int64{
			int64(rnd.Intn(3) + 1),
			int64(rnd.Intn(3) + 1),
			int64(rnd.Intn(3) + 1),
		}
		pos := [3]int64{
			int64(rnd.Intn(7) - 3),
			int64(rnd.Intn(7) - 3),
			int64(rnd.Intn(7) - 3),
		}

		var cells [][3]int64

		ok := true

		for x := pos[0]; x < pos[0]+size[0] && ok; x++ {
			for y := pos[1]; y < pos[1]+size[1] && ok; y++ {
				for z := pos[2]; z < pos[2]+size[2] && ok; z++ {
					c := [3]int64{x, y, z}
					if occupied[c] {
						ok = false
						break
					}

					cells = append(cells, c)
				}
			}
		}

		if !ok {
			continue
		}

		for _, c := range cells {
			occupied[c] = true
		}

		blocks = append(blocks, NewBlock(
			IV3(int(pos[0]), int(pos[1]), int(pos[2])),
			IV3(int(size[0]), int(size[1]), int(size[2])),
			meshTestRed))
	}

	if len(blocks) < 10 {
		t.Fatalf("only generated %d blocks", len(blocks))
	}

	want := meshCellArea(occupied)

	unmerged, err := blocks.Mesh()
	if err != nil {
		t.Fatalf("Blocks.Mesh() error = %v", err)
	}

	if got := unmerged.Area(); got != want {
		t.Fatalf("unmerged area = %v, want %v", got, want)
	}

	merged, err := blocks.Mesh(WithMeshMerge())
	if err != nil {
		t.Fatalf("Blocks.Mesh(WithMeshMerge()) error = %v", err)
	}

	if got := merged.Area(); got != want {
		t.Fatalf("merged area = %v, want %v", got, want)
	}

	if len(merged.Faces) > len(unmerged.Faces) {
		t.Fatalf("merging increased the face count from %d to %d",
			len(unmerged.Faces), len(merged.Faces))
	}

	// The merged mesh must not contain coincident faces either.
	for i, f := range merged.Faces {
		for j := i + 1; j < len(merged.Faces); j++ {
			if meshFacesCoincide(merged, f, merged.Faces[j]) {
				t.Fatalf("merged faces %d and %d coincide", i, j)
			}
		}
	}
}

// meshCellArea returns the exposed area of the given occupied unit cells.
func meshCellArea(occupied map[[3]int64]bool) float64 {
	dirs := [6][3]int64{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	}

	var area float64

	for c := range occupied {
		for _, d := range dirs {
			n := [3]int64{c[0] + d[0], c[1] + d[1], c[2] + d[2]}
			if !occupied[n] {
				area++
			}
		}
	}

	return area
}

func TestBlocksExportOBJ(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
		NewBlock(V3(1, 0, 0), V3(1, 1, 1), meshTestRed),
	}

	var buf bytes.Buffer

	if err := blocks.WriteOBJ(&buf, "blocks.mtl", WithMeshMerge()); err != nil {
		t.Fatalf("Blocks.WriteOBJ() error = %v", err)
	}

	m, err := blocks.Mesh(WithMeshMerge())
	if err != nil {
		t.Fatal(err)
	}

	var want bytes.Buffer

	if err := m.WriteOBJ(&want, "blocks.mtl"); err != nil {
		t.Fatal(err)
	}

	if buf.String() != want.String() {
		t.Fatalf("Blocks.WriteOBJ() differs from Mesh.WriteOBJ()")
	}

	dir := t.TempDir()
	objPath := filepath.Join(dir, "blocks.obj")
	mtlPath := filepath.Join(dir, "blocks.mtl")

	if err := blocks.ExportOBJ(objPath, mtlPath, WithMeshMerge()); err != nil {
		t.Fatalf("Blocks.ExportOBJ() error = %v", err)
	}

	if _, err := os.Stat(objPath); err != nil {
		t.Fatalf("OBJ was not written: %v", err)
	}

	if _, err := os.Stat(mtlPath); err != nil {
		t.Fatalf("MTL was not written: %v", err)
	}

	// Errors are propagated from the option validation.
	if err := blocks.WriteOBJ(&buf, "", WithMeshMaxFaces(1)); !errors.Is(err, ErrMeshFaceLimit) {
		t.Fatalf("Blocks.WriteOBJ() error = %v, want %v", err, ErrMeshFaceLimit)
	}
}

func TestNewBlockMesh(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(1, 1, 1), meshTestRed),
	}

	got, err := NewBlockMesh(blocks)
	if err != nil {
		t.Fatalf("NewBlockMesh() error = %v", err)
	}

	want := mustMesh(t, blocks)

	if !meshEqual(got, want) {
		t.Fatalf("NewBlockMesh() differs from Blocks.Mesh()")
	}

	if _, err := NewBlockMesh(Blocks{
		NewBlock(V3(0, 0, 0), V3(0, 1, 1), meshTestRed),
	}); !errors.Is(err, ErrMeshDegenerateBlock) {
		t.Fatalf("NewBlockMesh() error = %v, want %v", err, ErrMeshDegenerateBlock)
	}

	var nilMesh *Mesh

	if got := nilMesh.Area(); got != 0 {
		t.Fatalf("nil Mesh.Area() = %v, want 0", got)
	}
}
