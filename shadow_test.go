package gfx

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// pixelOf returns the pixel coordinate that the world point projects to.
func pixelOf(origin, p Vec3) image.Point {
	return blockCorner(p, origin).Pt()
}

// countShadowPixels returns the number of fully shadowed pixels and checks
// that every non-zero alpha in the mask is exactly 255 (a binary mask).
func countShadowPixels(t *testing.T, mask *image.Alpha) int {
	t.Helper()

	n := 0

	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			a := mask.AlphaAt(x, y).A

			if a != 0 {
				if a != 255 {
					t.Fatalf("mask alpha at (%v,%v) = %v, want 0 or 255", x, y, a)
				}

				n++
			}
		}
	}

	return n
}

func imagesEqual(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}

	r := a.Bounds()

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}

	return true
}

// twoWalls returns a tall wall (height 30) and a low wall (height 10) standing
// next to each other along the X axis, with the low wall in the long-shadow
// direction (+X) of the tall one.
func twoWalls() Blocks {
	return Blocks{
		NewBlock(V3(0, 0, 0), V3(10, 10, 30), BlockColorBlue),
		NewBlock(V3(20, 0, 0), V3(10, 10, 10), BlockColorRed),
	}
}

func TestProjectToPlaneAndReach(t *testing.T) {
	// A top corner 40 units up hits the ground 40 units along the horizontal
	// sun direction for a 45 degree sun, 10 units for the steep sun.
	got := projectToPlane(V3(0, 0, 40), 0, V3(-1, 0, 1))

	if !got.Eq(V3(40, 0, 0)) {
		t.Fatalf("projectToPlane = %v, want gfx.V3(40, 0, 0)", got)
	}

	got = projectToPlane(V3(0, 0, 40), 0, V3(-1, 0, 4))

	// Steep sun: the ray drops 40 units over only 10 horizontal units.
	if want := V3(10, 0, 0); !got.Eq(want) {
		t.Fatalf("projectToPlane = %v, want %v", got, want)
	}

	// Raising onto a plane above the corner keeps the corner in place.
	got = projectToPlane(V3(5, 6, 2), 10, V3(-1, 0, 1))

	if want := V3(5, 6, 2); !got.Eq(want) {
		t.Fatalf("projectToPlane below plane = %v, want %v", got, want)
	}

	// Reach box of the tall wall under the 45 degree sun spans X 0..40.
	reach := shadowReachBox(V3(0, 0, 0), V3(10, 10, 30), V3(-1, 0, 1))

	if reach.Min.X != 0 || reach.Max.X != 40 || reach.Min.Y != 0 || reach.Max.Y != 10 {
		t.Fatalf("reach = %v, want X 0..40, Y 0..10", reach)
	}
}

// polygonArea returns the absolute shoelace area of a screen polygon.
func polygonArea(p Polygon) float64 {
	var sum float64

	for i := range p {
		a, b := p[i], p[(i+1)%len(p)]
		sum += a.X*b.Y - b.X*a.Y
	}

	return math.Abs(sum) / 2
}

func TestShadowHullLength(t *testing.T) {
	origin := V3(200, -200, 1)
	sun := V3(-1, 0, 1)

	sbs := prepareShadowBlocks(Blocks{NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorBlue)}, origin, sun)
	hull := shadowHull(sbs[0], 0)

	// The 45 degree sun stretches the 10-unit cube into the world rectangle
	// [0,20] x [0,10] on the ground. Check the hull against that rectangle
	// through containment and area rather than exact corner count: monotone
	// chain may keep a collinear split point from floating point rounding.
	want := Polygon{
		blockCorner(V3(0, 0, 0), origin),
		blockCorner(V3(20, 0, 0), origin),
		blockCorner(V3(20, 10, 0), origin),
		blockCorner(V3(0, 10, 0), origin),
	}

	// Every expected rectangle corner is a hull vertex (within rounding).
	for _, p := range want {
		found := false

		for _, q := range hull {
			if math.Abs(q.X-p.X) < 1e-6 && math.Abs(q.Y-p.Y) < 1e-6 {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("hull %v is missing expected corner %v", hull, p)
		}
	}

	// The hull covers exactly the expected rectangle: equal area and the same
	// sampled interior points, so extra collinear vertices add nothing.
	if a, w := polygonArea(hull), polygonArea(want); math.Abs(a-w) > 1e-6 {
		t.Fatalf("hull area = %v, want %v", a, w)
	}

	for _, wp := range []Vec3{V3(5, 5, 0), V3(15, 5, 0)} {
		if !blockCorner(wp, origin).In(hull) {
			t.Fatalf("hull %v does not cover %v", hull, wp)
		}
	}

	// Nothing past the far edge of the expected rectangle is covered.
	if blockCorner(V3(21, 5, 0), origin).In(hull) {
		t.Fatal("hull extends beyond the expected shadow length")
	}

	// The steep sun drops the top corners 10 Z units over 10/4 = 2.5
	// horizontal units, so the ground shadow spans world X [0,12.5]. Verify
	// against the hull's screen bounds, rotation-independent on screen.
	sbs = prepareShadowBlocks(Blocks{NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorBlue)}, origin, V3(-1, 0, 4))
	hull = shadowHull(sbs[0], 0)

	steepWant := Polygon{
		blockCorner(V3(0, 0, 0), origin),
		blockCorner(V3(12.5, 0, 0), origin),
		blockCorner(V3(12.5, 10, 0), origin),
		blockCorner(V3(0, 10, 0), origin),
	}

	hb, wb := hull.Rect(), steepWant.Rect()

	if math.Abs(hb.Min.X-wb.Min.X) > 1e-6 || math.Abs(hb.Max.X-wb.Max.X) > 1e-6 ||
		math.Abs(hb.Min.Y-wb.Min.Y) > 1e-6 || math.Abs(hb.Max.Y-wb.Max.Y) > 1e-6 {
		t.Fatalf("steep-sun hull bounds = %v, want %v", hb, wb)
	}
}

func TestShadowTwoWallsThreeSuns(t *testing.T) {
	origin := V3(300, -300, 1)
	bounds := IR(0, 0, 640, 480)
	blocks := twoWalls()
	lowTop := blocks[1].Corners(origin).Top()

	// Average of the four top corners is the face center, strictly inside it.
	var sum Vec

	for _, v := range lowTop {
		sum = sum.Add(v)
	}

	lowTopCenter := sum.Scaled(1 / float64(len(lowTop)))
	centerPt := image.Pt(int(math.Round(lowTopCenter.X)), int(math.Round(lowTopCenter.Y)))

	// Make sure the sampled pixel really belongs to the low wall's top face.
	if !IV(centerPt.X, centerPt.Y).In(lowTop) {
		t.Fatalf("test setup: %v not inside low top %v", centerPt, lowTop)
	}

	for _, tc := range []struct {
		name       string
		sun        Vec3
		wantOnTop  bool
		groundFarX float64 // world X extent of the tall wall's ground shadow
	}{
		{"long low sun", V3(-1, 0, 1), true, 40},
		{"steep sun", V3(-1, 0, 4), false, 17.5},
		{"reverse sun", V3(1, 0, 1), false, 0}, // shadow stretches toward -X
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := blocks.ShadowMask(bounds, origin, tc.sun)

			if got := mask.AlphaAt(centerPt.X, centerPt.Y).A == 255; got != tc.wantOnTop {
				t.Fatalf("rooftop shadow at low top = %v, want %v", got, tc.wantOnTop)
			}

			sbs := prepareShadowBlocks(blocks, origin, tc.sun)
			hull := shadowHull(sbs[0], 0)

			// Ground shadow length: a point well inside the expected extent
			// must be covered by the hull.
			var probe Vec3

			switch tc.name {
			case "reverse sun":
				probe = V3(-20, 5, 0) // tall wall footprint ends at X = 0
			default:
				probe = V3(tc.groundFarX-2, 5, 0)
			}

			probeV := blockCorner(probe, origin)
			if !probeV.In(hull) {
				t.Fatalf("ground hull %v does not cover expected extent %v", hull, probe)
			}

			if n := countShadowPixels(t, mask); n == 0 {
				t.Fatal("expected shadow pixels, got empty mask")
			}
		})
	}
}

func TestShadowHorizontalAndZeroSun(t *testing.T) {
	origin := V3(300, -300, 1)
	bounds := IR(0, 0, 640, 480)
	blocks := twoWalls()

	for _, sun := range []Vec3{V3(0, 0, 0), V3(-1, 0, 0), V3(1, -1, 0)} {
		mask := blocks.ShadowMask(bounds, origin, sun)

		if n := countShadowPixels(t, mask); n != 0 {
			t.Fatalf("sun %v produced %v shadow pixels, want 0", sun, n)
		}
	}
}

func TestShadowDoesNotChangeDrawnPixelsWhenDisabled(t *testing.T) {
	origin := V3(300, -300, 1)
	bg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	with := NewImage(640, 480, bg)
	without := NewImage(640, 480, bg)

	blocks := twoWalls()
	blocks.Sort()
	blocks.Draw(with, origin)
	blocks.Draw(without, origin)

	// Building a mask must never touch the destination.
	mask := blocks.ShadowMask(with.Bounds(), origin, V3(-1, 0, 1))
	if !imagesEqual(with, without) {
		t.Fatal("ShadowMask modified the destination image")
	}

	// A horizontal sun yields an empty mask, so compositing is a no-op.
	blocks.DrawShadow(with, origin, V3(-1, 0, 0), nil)

	if !imagesEqual(with, without) {
		t.Fatal("DrawShadow with a horizontal sun changed the drawn pixels")
	}

	if mask.Bounds() != with.Bounds() {
		t.Fatalf("mask bounds = %v, want %v", mask.Bounds(), with.Bounds())
	}
}

func TestDrawShadowAlphaAndNoDoubleDarkening(t *testing.T) {
	origin := V3(300, -300, 1)
	bounds := IR(0, 0, 640, 480)

	// Two casters whose ground shadows overlap on the same ground pixel.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorBlue),
		NewBlock(V3(2, 2, 0), V3(10, 10, 10), BlockColorRed),
	}

	mask := blocks.ShadowMask(bounds, origin, V3(-1, 0, 1))

	// Every mask pixel is binary: overlaps are merged, never accumulated.
	if n := countShadowPixels(t, mask); n == 0 {
		t.Fatal("expected shadow pixels")
	}

	dst := NewImage(640, 480, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	blocks.DrawShadow(dst, origin, V3(-1, 0, 1), color.NRGBA{A: 128})

	// Find a shadow pixel and check the exact source-over blend result.
	found := false

	for y := 180; y < 320 && !found; y++ {
		for x := 200; x < 460; x++ {
			if mask.AlphaAt(x, y).A == 255 {
				r, _, _, _ := dst.At(x, y).RGBA()

				// Black at 128/255 over white: 255 * (255-128) / 255 = 127.
				if got := uint8(r >> 8); got != 127 {
					t.Fatalf("shadow pixel blend = %v, want 127", got)
				}

				found = true

				break
			}
		}
	}

	if !found {
		t.Fatal("found no shadow pixel in the expected region")
	}
}

func TestShadowFullyOccluded(t *testing.T) {
	origin := V3(300, -300, 1)
	bounds := IR(0, 0, 640, 480)

	// The low caster's whole 45 degree ground shadow lands on the footprint
	// of the tall block standing directly in front of it.
	blocks := Blocks{
		NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorBlue),
		NewBlock(V3(10, 0, 0), V3(10, 10, 30), BlockColorRed),
	}

	mask := blocks.ShadowMask(bounds, origin, V3(-1, 0, 1))

	// Pixels of the tall block's footprint must be unshadowed: the low
	// caster's shadow is hidden behind it and the low block cannot shadow
	// the taller block's top.
	for xw := 10.0; xw < 20; xw++ {
		for yw := 0.0; yw < 10; yw++ {
			pt := pixelOf(origin, V3(xw+0.5, yw+0.5, 0))
			if a := mask.AlphaAt(pt.X, pt.Y).A; a != 0 {
				t.Fatalf("occluded footprint at world (%v,%v) has shadow alpha %v", xw, yw, a)
			}
		}
	}

	// The tall block's own shadow still reaches the open ground past X = 20.
	probe := pixelOf(origin, V3(35, 5, 0))
	if a := mask.AlphaAt(probe.X, probe.Y).A; a != 255 {
		t.Fatalf("open ground shadow alpha = %v, want 255", a)
	}
}

func TestShadowDegenerateAndBelowGroundBlocks(t *testing.T) {
	origin := V3(200, -200, 1)
	bounds := IR(0, 0, 640, 480)
	sun := V3(-1, 0, 1)

	for _, b := range []Block{
		NewBlock(V3(0, 0, 0), V3(0, 10, 10), BlockColorBlue),
		NewBlock(V3(0, 0, 0), V3(10, -1, 10), BlockColorBlue),
		NewBlock(V3(0, 0, 0), V3(10, 10, 0), BlockColorBlue),
	} {
		if sbs := prepareShadowBlocks(Blocks{b}, origin, sun); len(sbs) != 0 {
			t.Fatalf("degenerate block %v was not skipped", b)
		}

		if n := countShadowPixels(t, Blocks{b}.ShadowMask(bounds, origin, sun)); n != 0 {
			t.Fatalf("degenerate block %v cast a shadow", b)
		}
	}

	// A block entirely below ground cannot shadow the ground plane.
	buried := Blocks{NewBlock(V3(0, 0, -20), V3(10, 10, 5), BlockColorBlue)}

	sbs := prepareShadowBlocks(buried, origin, sun)
	if hull := shadowHull(sbs[0], 0); hull != nil {
		t.Fatalf("below-ground block produced hull %v, want nil", hull)
	}

	if n := countShadowPixels(t, buried.ShadowMask(bounds, origin, sun)); n != 0 {
		t.Fatal("below-ground block cast a ground shadow")
	}
}

func TestShadowNegativeCoordinates(t *testing.T) {
	origin := V3(300, -300, 1)
	bounds := IR(0, 0, 640, 480)

	blocks := Blocks{
		NewBlock(V3(-40, -40, -5), V3(10, 10, 15), BlockColorBlue),
		NewBlock(V3(-20, -40, 0), V3(10, 10, 8), BlockColorRed),
	}

	mask := blocks.ShadowMask(bounds, origin, V3(-1, 0, 1))

	if n := countShadowPixels(t, mask); n == 0 {
		t.Fatal("negative-coordinate blocks produced no shadow")
	}
}

func TestShadowOutsideViewportAndTranslatedBounds(t *testing.T) {
	origin := V3(300, -300, 1)
	blocks := Blocks{NewBlock(V3(0, 0, 0), V3(10, 10, 10), BlockColorBlue)}

	// Tiny viewport nowhere near the projected shadow.
	mask := blocks.ShadowMask(IR(0, 0, 4, 4), origin, V3(-1, 0, 1))

	if n := countShadowPixels(t, mask); n != 0 {
		t.Fatalf("off-viewport shadow produced %v pixels", n)
	}

	// Translated bounds must be indexed correctly (no panic, correct alpha).
	translated := IR(100, 100, 700, 500)
	mask = blocks.ShadowMask(translated, origin, V3(-1, 0, 1))

	if mask.Bounds() != translated {
		t.Fatalf("mask bounds = %v, want %v", mask.Bounds(), translated)
	}

	if n := countShadowPixels(t, mask); n == 0 {
		t.Fatal("translated viewport contains no shadow pixels")
	}
}

func TestShadowHugeSceneIsBounded(t *testing.T) {
	origin := V3(0, 0, 1)
	bounds := IR(0, 0, 64, 64)

	// A block a million units away with a nearly horizontal sun: the projected
	// hull is gigantic, but clamping and viewport clipping keep the work finite.
	blocks := Blocks{
		NewBlock(V3(1e6, 1e6, 0), V3(10, 10, 1e6), BlockColorBlue),
	}

	mask := blocks.ShadowMask(bounds, origin, V3(-1, 1, 1e-9))

	for _, p := range mask.Pix {
		if p != 0 && p != 255 {
			t.Fatalf("unexpected mask alpha %v", p)
		}
	}
}

func TestConvexHull(t *testing.T) {
	// Square with interior and duplicate points.
	square := Polygon{
		V(0, 0), V(10, 0), V(10, 10), V(0, 10),
		V(5, 5), V(5, 5), V(10, 0),
	}

	hull := convexHull(append(Polygon(nil), square...))

	if len(hull) != 4 {
		t.Fatalf("hull = %v, want 4 corners", hull)
	}

	// All-collinear points cannot form an area.
	if h := convexHull(Polygon{V(0, 0), V(1, 1), V(2, 2)}); h != nil {
		t.Fatalf("collinear hull = %v, want nil", h)
	}

	// Fewer than three points.
	if h := convexHull(Polygon{V(0, 0), V(1, 1)}); h != nil {
		t.Fatalf("two-point hull = %v, want nil", h)
	}
}
