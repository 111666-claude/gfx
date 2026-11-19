package gfx

import (
	"math"
	"reflect"
	"testing"
)

// pickTestScene returns three staggered blocks, with different sizes, heights
// and negative coordinates, that overlap in the screen projection.
func pickTestScene() Blocks {
	return Blocks{
		NewBlock(V3(-80, -60, 0), V3(120, 100, 60), BlockColorRed),
		NewBlock(V3(0, 10, 24), V3(100, 90, 90), BlockColorGreen),
		NewBlock(V3(40, -40, 0), V3(90, 120, 140), BlockColorBlue),
	}
}

// pickTestPalette returns a palette with the 9 block face colors, and the
// index of a background color that is not used by any block face.
func pickTestPalette() (Palette, int) {
	return PaletteTango[:10], 9
}

// pickTestFaceIndex returns the palette index used for a block face.
func pickTestFaceIndex(block int, face BlockFace) int {
	return 3*block + int(face) - 1
}

// pickTestFaceAt returns the block and face that paint a palette index.
func pickTestFaceAt(t *testing.T, index int) (int, BlockFace) {
	t.Helper()

	if index < 0 || index > 8 {
		t.Fatalf("palette index %d does not belong to a block face", index)
	}

	return index / 3, BlockFace(index%3 + 1)
}

func pickTestProjected(u Vec3, origin Vec3) Vec {
	return blockCorner(u, origin)
}

// pickTestPolygonCenter returns the average of the polygon vertices.
func pickTestPolygonCenter(p Polygon) Vec {
	var c Vec

	for _, u := range p {
		c = c.Add(u)
	}

	return c.Scaled(1 / float64(len(p)))
}

func pickTestNearlyEqual(a, b Vec, eps float64) bool {
	return math.Abs(a.X-b.X) <= eps && math.Abs(a.Y-b.Y) <= eps
}

// checkBlockHit verifies that the point of a hit lies on the reported face,
// projects back to the picked screen coordinate, and that the depth matches
// the point.
func checkBlockHit(t *testing.T, blocks Blocks, hit BlockHit, p Vec, origin Vec3) {
	t.Helper()

	if hit.Index < 0 || hit.Index >= len(blocks) {
		t.Fatalf("hit.Index = %d, want in [0, %d)", hit.Index, len(blocks))
	}

	b := blocks[hit.Index]
	pt := hit.Point

	const eps = 1e-6

	inRange := func(v, min, max float64) bool {
		return v >= min-eps && v <= max+eps
	}

	switch hit.Face {
	case BlockFaceTop:
		if math.Abs(pt.Z-(b.Pos.Z+b.Size.Z)) > eps {
			t.Errorf("top point %v, want Z = %v", pt, b.Pos.Z+b.Size.Z)
		}
		if !inRange(pt.X, b.Pos.X, b.Pos.X+b.Size.X) || !inRange(pt.Y, b.Pos.Y, b.Pos.Y+b.Size.Y) {
			t.Errorf("top point %v outside face of %v", pt, b)
		}
	case BlockFaceLeft:
		if math.Abs(pt.X-b.Pos.X) > eps {
			t.Errorf("left point %v, want X = %v", pt, b.Pos.X)
		}
		if !inRange(pt.Y, b.Pos.Y, b.Pos.Y+b.Size.Y) || !inRange(pt.Z, b.Pos.Z, b.Pos.Z+b.Size.Z) {
			t.Errorf("left point %v outside face of %v", pt, b)
		}
	case BlockFaceRight:
		if math.Abs(pt.Y-b.Pos.Y) > eps {
			t.Errorf("right point %v, want Y = %v", pt, b.Pos.Y)
		}
		if !inRange(pt.X, b.Pos.X, b.Pos.X+b.Size.X) || !inRange(pt.Z, b.Pos.Z, b.Pos.Z+b.Size.Z) {
			t.Errorf("right point %v outside face of %v", pt, b)
		}
	default:
		t.Fatalf("hit.Face = %v, want a known face", hit.Face)
	}

	if got := pickTestProjected(pt, origin); !pickTestNearlyEqual(got, p, 1e-6) {
		t.Fatalf("blockCorner(%v) = %v, want %v", pt, got, p)
	}

	if want := blockDepth(pt); hit.Depth != want {
		t.Fatalf("hit.Depth = %v, want %v", hit.Depth, want)
	}
}

// TestBlocksPickMatchesDraw renders three staggered blocks and checks, pixel
// by pixel, that Blocks.Pick returns the block and face that Blocks.Draw
// painted there.
func TestBlocksPickMatchesDraw(t *testing.T) {
	const (
		width, height = 560, 680
	)

	origin := V3(260, -420, 1)

	blocks := pickTestScene()

	// One palette index per block face, with a color no block uses as the
	// background, so the rendered index tells which face painted a pixel.
	pal, bg := pickTestPalette()

	for i := range blocks {
		blocks[i].Color = BlockColor{
			Light:  pal[pickTestFaceIndex(i, BlockFaceTop)],
			Medium: pal[pickTestFaceIndex(i, BlockFaceRight)],
			Dark:   pal[pickTestFaceIndex(i, BlockFaceLeft)],
		}
	}

	before := make(Blocks, len(blocks))
	copy(before, blocks)

	dst := NewPaletted(width, height, pal, pal[bg])

	blocks.Draw(dst, origin)

	facePixels := map[BlockFace]int{}
	blockPixels := map[int]int{}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			u := IV(x, y)
			ci := int(dst.ColorIndexAt(x, y))

			hit, ok := blocks.Pick(u, origin)

			if ci == bg {
				if ok {
					t.Fatalf("Pick(%v) = %+v, want no hit on background pixel", u, hit)
				}

				continue
			}

			wantBlock, wantFace := pickTestFaceAt(t, ci)

			if !ok {
				t.Fatalf("Pick(%v) found nothing, want block %d face %v", u, wantBlock, wantFace)
			}

			if hit.Index != wantBlock || hit.Face != wantFace {
				t.Fatalf("Pick(%v) = block %d face %v, want block %d face %v (palette index %d)",
					u, hit.Index, hit.Face, wantBlock, wantFace, ci)
			}

			checkBlockHit(t, blocks, hit, u, origin)

			facePixels[hit.Face]++
			blockPixels[hit.Index]++
		}
	}

	for i := range blocks {
		if blockPixels[i] == 0 {
			t.Errorf("block %d was never picked, scene does not cover it", i)
		}
	}

	for _, f := range []BlockFace{BlockFaceTop, BlockFaceLeft, BlockFaceRight} {
		if facePixels[f] == 0 {
			t.Errorf("face %v was never picked, scene does not show it", f)
		}
	}

	if !reflect.DeepEqual(blocks, before) {
		t.Fatal("Blocks.Pick modified the Blocks slice")
	}

	// The same holds when the scene is drawn with the blocks sorted.
	sorted := append(Blocks(nil), before...)
	sorted.Sort()

	sortedBefore := append(Blocks(nil), sorted...)

	dst = NewPaletted(width, height, pal, pal[bg])
	sorted.Draw(dst, origin)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ci := int(dst.ColorIndexAt(x, y))
			if ci == bg {
				continue
			}

			wantBlock, wantFace := pickTestFaceAt(t, ci)

			// The palette index follows the rendered face, so the index
			// in the sorted slice maps back to the original block.
			hit, ok := sorted.Pick(IV(x, y), origin)
			if !ok || hit.Face != wantFace || sorted[hit.Index] != before[wantBlock] {
				t.Fatalf("sorted Pick(%d, %d) = %+v, %v; want face %v of block %d",
					x, y, hit, ok, wantFace, wantBlock)
			}
		}
	}

	if !reflect.DeepEqual(sorted, sortedBefore) {
		t.Fatal("Blocks.Pick modified the sorted Blocks slice")
	}
}

func TestBlocksPickEmpty(t *testing.T) {
	origin := V3(100, -100, 1)

	for _, blocks := range []Blocks{nil, {}} {
		hit, ok := blocks.Pick(V(0, 0), origin)

		if ok || hit.Index != -1 {
			t.Fatalf("Pick on empty Blocks = %+v, %v; want Index -1, false", hit, ok)
		}
	}
}

func TestBlocksPickInvalid(t *testing.T) {
	origin := V3(100, -100, 1)
	blocks := Blocks{NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorRed)}

	center := pickTestProjected(V3(0, 0, 20), origin)
	if _, ok := blocks.Pick(center, origin); !ok {
		t.Fatal("Pick at the top face center found nothing")
	}

	nan, inf := math.NaN(), math.Inf(1)

	for _, tc := range []struct {
		name   string
		p      Vec
		origin Vec3
	}{
		{"NaN p.X", V(nan, center.Y), origin},
		{"NaN p.Y", V(center.X, nan), origin},
		{"Inf p.X", V(inf, center.Y), origin},
		{"NaN origin.X", center, V3(nan, origin.Y, origin.Z)},
		{"Inf origin.Y", center, V3(origin.X, inf, origin.Z)},
		{"NaN origin.Z", center, V3(origin.X, origin.Y, nan)},
		{"Inf origin.Z", center, V3(origin.X, origin.Y, inf)},
		{"zero origin.Z", center, V3(origin.X, origin.Y, 0)},
		{"negative origin.Z", center, V3(origin.X, origin.Y, -1)},
		{"negative infinite origin.Z", center, V3(origin.X, origin.Y, math.Inf(-1))},
	} {
		hit, ok := blocks.Pick(tc.p, tc.origin)

		if ok || hit.Index != -1 {
			t.Errorf("%s: Pick = %+v, %v; want Index -1, false", tc.name, hit, ok)
		}
	}

	// A block with non-finite coordinates is not pickable, but must not
	// prevent other blocks from being picked.
	bad := Blocks{
		NewBlock(V3(nan, 0, 0), V3(20, 20, 20), BlockColorRed),
		NewBlock(V3(0, inf, 0), V3(20, 20, 20), BlockColorRed),
		NewBlock(V3(0, 0, 0), V3(nan, 20, 20), BlockColorRed),
		NewBlock(V3(0, 0, 0), V3(20, inf, 20), BlockColorRed),
		blocks[0],
	}

	hit, ok := bad.Pick(center, origin)
	if !ok || hit.Index != 4 {
		t.Fatalf("Pick with invalid blocks = %+v, %v; want Index 4, true", hit, ok)
	}
}

// TestBlocksPickDegenerate covers blocks that paint no pixels or only some of
// their faces because one or more size components are zero.
func TestBlocksPickDegenerate(t *testing.T) {
	origin := V3(100, -100, 1)
	color := BlockColorRed

	for _, tc := range []struct {
		name      string
		pos, size Vec3
		faces     []BlockFace
	}{
		{"all zero", V3(0, 0, 0), V3(0, 0, 0), nil},
		{"zero X", V3(0, 0, 0), V3(0, 40, 40), []BlockFace{BlockFaceLeft}},
		{"zero Y", V3(0, 0, 0), V3(40, 0, 40), []BlockFace{BlockFaceRight}},
		{"zero Z", V3(0, 0, 0), V3(40, 40, 0), []BlockFace{BlockFaceTop}},
		{"zero X and Y", V3(0, 0, 0), V3(0, 0, 40), nil},
		{"zero X and Z", V3(0, 0, 0), V3(0, 40, 0), nil},
		{"zero Y and Z", V3(0, 0, 0), V3(40, 0, 0), nil},
		{"negative X", V3(0, 0, 0), V3(-40, 40, 40), nil},
		{"negative Y", V3(0, 0, 0), V3(40, -40, 40), nil},
		{"negative Z", V3(0, 0, 0), V3(40, 40, -40), nil},
		{"negative size", V3(0, 0, 0), V3(-40, -40, -40), nil},
	} {
		blocks := Blocks{NewBlock(tc.pos, tc.size, color)}

		seen := map[BlockFace]bool{}

		// Scan an area around the block, as Block.Rect is not normalized
		// for blocks with a negative size.
		const margin = 60

		r := blocks[0].Rect(origin)
		for y := int(r.Min.Y) - margin; y <= int(r.Max.Y)+margin; y++ {
			for x := int(r.Min.X) - margin; x <= int(r.Max.X)+margin; x++ {
				hit, ok := blocks.Pick(IV(x, y), origin)
				if !ok {
					continue
				}

				seen[hit.Face] = true

				if !containsBlockFace(tc.faces, hit.Face) {
					t.Fatalf("%s: Pick(%d, %d) = face %v, want only %v",
						tc.name, x, y, hit.Face, tc.faces)
				}

				checkBlockHit(t, blocks, hit, IV(x, y), origin)
			}
		}

		for _, f := range tc.faces {
			if !seen[f] {
				t.Errorf("%s: face %v was never picked", tc.name, f)
			}
		}
	}
}

func containsBlockFace(faces []BlockFace, f BlockFace) bool {
	for _, want := range faces {
		if want == f {
			return true
		}
	}

	return false
}

// TestBlocksPickFaceCenters checks each face polygon center is picked as the
// expected face and that the point is on that face.
func TestBlocksPickFaceCenters(t *testing.T) {
	origin := V3(120, -120, 1.25)
	blocks := Blocks{NewBlock(V3(-15, -5, 3), V3(40, 30, 25), BlockColorGreen)}

	_, top, left, right := blocks[0].Polygons(origin)

	for _, tc := range []struct {
		face BlockFace
		poly Polygon
	}{
		{BlockFaceTop, top},
		{BlockFaceLeft, left},
		{BlockFaceRight, right},
	} {
		p := pickTestPolygonCenter(tc.poly)

		hit, ok := blocks.Pick(p, origin)
		if !ok {
			t.Fatalf("Pick at center of %v face found nothing", tc.face)
		}

		if hit.Face != tc.face {
			t.Fatalf("Pick at center of %v face = %v", tc.face, hit.Face)
		}

		checkBlockHit(t, blocks, hit, p, origin)
	}
}

// TestBlocksPickTangency checks points shared by several blocks and faces at
// their boundaries, where the choice has to be stable.
func TestBlocksPickTangency(t *testing.T) {
	origin := V3(100, -100, 1)

	// Two blocks that share the plane X = 20, so their projections touch on
	// screen without any pixel in between.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorGreen),
		NewBlock(V3(20, 0, 0), V3(20, 20, 20), BlockColorBlue),
	}

	shared := []Vec3{
		V3(20, 0, 0), V3(20, 0, 10), V3(20, 0, 20),
		V3(20, 20, 0), V3(20, 20, 10), V3(20, 20, 20),
	}

	for _, u := range shared {
		p := pickTestProjected(u, origin)

		hit, ok := blocks.Pick(p, origin)
		if !ok {
			t.Fatalf("Pick at shared point %v found nothing", u)
		}

		// The second block is drawn last and therefore covers the seam.
		if hit.Index != 1 {
			t.Fatalf("Pick at shared point %v = block %d, want block 1 (drawn last)", u, hit.Index)
		}

		checkBlockHit(t, blocks, hit, p, origin)

		again, ok := blocks.Pick(p, origin)
		if !ok || again != hit {
			t.Fatalf("Pick at shared point %v is not stable: %+v then %+v (%v)", u, hit, again, ok)
		}
	}

	// Points in the middle of the edges shared by two faces of one block.
	// The last triangle to cover the point wins, and the result is stable.
	single := Blocks{NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorGreen)}
	c := single[0].Corners(origin)

	for _, edge := range [][2]Vec{
		{c.LeftUp, c.FrontUp},    // top/left
		{c.FrontUp, c.RightUp},   // top/right
		{c.FrontUp, c.FrontDown}, // left/right
	} {
		p := edge[0].Lerp(edge[1], 0.5)

		hit, ok := single.Pick(p, origin)
		if !ok {
			t.Fatalf("Pick at edge midpoint %v found nothing", p)
		}

		checkBlockHit(t, single, hit, p, origin)

		if again, _ := single.Pick(p, origin); again != hit {
			t.Fatalf("Pick at edge midpoint %v is not stable: %+v then %+v", p, hit, again)
		}
	}

	// The outer corner of the front vertical edge is shared by all three
	// blocks: block 1 (left), block 2 (right) and the three faces of the
	// first block.
	p := c.FrontUp

	hit, ok := blocks.Pick(p, origin)
	if !ok || hit.Index != 1 {
		t.Fatalf("Pick at outer corner = %+v, %v; want block 1", hit, ok)
	}

	checkBlockHit(t, blocks, hit, p, origin)
}

// TestBlocksPickOccluded checks that a completely hidden block is not picked
// and that a tie in depth resolves to the block drawn last.
func TestBlocksPickOccluded(t *testing.T) {
	origin := V3(100, -100, 1)

	inner := NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorGreen)
	outer := NewBlock(V3(0, 0, 0), V3(30, 30, 30), BlockColorBlue)

	hidden := Blocks{inner, outer}

	r := inner.Rect(origin).Bounds()

	for y := r.Min.Y - 1; y <= r.Max.Y+1; y++ {
		for x := r.Min.X - 1; x <= r.Max.X+1; x++ {
			hit, ok := hidden.Pick(IV(x, y), origin)
			if ok && hit.Index == 0 {
				t.Fatalf("Pick(%d, %d) = hidden block 0, want block 1 or no hit", x, y)
			}
		}
	}

	// Without the block in front the inner block is pickable.
	only := Blocks{inner}
	if _, ok := only.Pick(pickTestProjected(V3(0, 0, 10), origin), origin); !ok {
		t.Fatal("inner block is not pickable on its own")
	}

	// Same position and size: the block drawn last wins.
	tie := Blocks{
		NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorGreen),
		NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorBlue),
	}

	p := pickTestProjected(V3(0, 0, 20), origin)

	hit, ok := tie.Pick(p, origin)
	if !ok || hit.Index != 1 || hit.Face != BlockFaceTop {
		t.Fatalf("Pick on same depth blocks = %+v, %v; want block 1 top", hit, ok)
	}

	// Swapping the order picks the other block: the last one drawn wins,
	// leaving the face and the depth alone.
	swapped := Blocks{tie[1], tie[0]}

	hit2, ok := swapped.Pick(p, origin)
	if !ok || hit2.Index != 1 || hit2.Face != hit.Face {
		t.Fatalf("Pick on swapped same depth blocks = %+v, %v; want Index 1, face %v", hit2, ok, hit.Face)
	}

	if hit2.Depth != hit.Depth {
		t.Fatalf("same depth blocks picked at different depths: %v and %v", hit.Depth, hit2.Depth)
	}
}

// TestBlocksPickDepth checks that depth grows along the view direction and
// that the picked point is the point of the visible block on that view ray.
func TestBlocksPickDepth(t *testing.T) {
	origin := V3(100, -100, 1)

	// (1, 1, -1) is the (unnormalized) view direction, so moving a block
	// by it keeps its screen projection unchanged.
	near := NewBlock(V3(0, 0, 0), V3(20, 20, 20), BlockColorGreen)
	far := near
	far.Pos = near.Pos.Add(V3(1, 1, -1))

	p := pickTestProjected(V3(0, 0, 20), origin)

	// Draw far first, near last: the near block is visible.
	hitNear, ok := Blocks{far, near}.Pick(p, origin)
	if !ok || hitNear.Index != 1 {
		t.Fatalf("Pick = %+v, %v; want block 1", hitNear, ok)
	}

	// Draw near first, far last: far covers the pixel even though it is
	// farther away, matching Blocks.Draw.
	hitFar, ok := Blocks{near, far}.Pick(p, origin)
	if !ok || hitFar.Index != 1 {
		t.Fatalf("Pick = %+v, %v; want block 1", hitFar, ok)
	}

	if !(hitNear.Depth < hitFar.Depth) {
		t.Fatalf("depth of near block %v not smaller than far block %v", hitNear.Depth, hitFar.Depth)
	}

	if got, want := hitFar.Depth-hitNear.Depth, math.Sqrt(3); math.Abs(got-want) > 1e-9 {
		t.Fatalf("depth difference = %v, want %v", got, want)
	}

	if got, want := hitFar.Point, hitNear.Point.Add(V3(1, 1, -1)); !got.Eq(want) {
		t.Fatalf("far hit point = %v, want %v", got, want)
	}
}

// TestBlocksPickPixelEdge checks that Pick is consistent with the rendered
// pixels along the silhouette of blocks: a pixel covered by the block is
// picked as that block, and a pixel just outside it is not.
func TestBlocksPickPixelEdge(t *testing.T) {
	blocks := Blocks{
		NewBlock(V3(-20, -10, 5), V3(35, 25, 18), BlockColorGreen),
		NewBlock(V3(6, -30, 0), V3(40, 22, 30), BlockColorBlue),
	}

	for _, origin := range []Vec3{
		V3(120, -80, 1),
		V3(97.5, -63.25, 0.75),
		V3(140, -100, 1.5),
	} {
		pal, bg := pickTestPalette()

		colored := make(Blocks, len(blocks))
		copy(colored, blocks)

		for i := range colored {
			colored[i].Color = BlockColor{
				Light:  pal[pickTestFaceIndex(i, BlockFaceTop)],
				Medium: pal[pickTestFaceIndex(i, BlockFaceRight)],
				Dark:   pal[pickTestFaceIndex(i, BlockFaceLeft)],
			}
		}

		const w, h = 420, 420

		dst := NewPaletted(w, h, pal, pal[bg])
		colored.Draw(dst, origin)

		// Compare only near the block edges: every pixel whose 4-neighbours
		// have a different palette index.
		for y := 1; y < h-1; y++ {
			for x := 1; x < w-1; x++ {
				ci := int(dst.ColorIndexAt(x, y))

				isEdge := false
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if int(dst.ColorIndexAt(x+d[0], y+d[1])) != ci {
						isEdge = true
					}
				}

				if !isEdge {
					continue
				}

				hit, ok := colored.Pick(IV(x, y), origin)

				if ci == bg {
					if ok {
						t.Fatalf("origin %v: Pick(%d, %d) = %+v on background edge pixel", origin, x, y, hit)
					}

					continue
				}

				wantBlock, wantFace := pickTestFaceAt(t, ci)

				if !ok || hit.Index != wantBlock || hit.Face != wantFace {
					t.Fatalf("origin %v: Pick(%d, %d) = %+v, %v; want block %d face %v",
						origin, x, y, hit, ok, wantBlock, wantFace)
				}
			}
		}
	}
}
