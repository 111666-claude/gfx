package gfx

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
)

// BlockShadowColor is the default translucent color used when drawing shadows.
var BlockShadowColor = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0x80}

// shadowCoordLimit clamps projected screen coordinates. A scene that would
// project beyond this is treated as off-viewport instead of overflowing into
// Inf/NaN coordinates, which keeps every shadow computation finite and bounded.
const shadowCoordLimit = 1e9

// ShadowMask computes the shadows cast by the blocks for a sun direction and
// returns them as an alpha mask the size of bounds.
//
// The sun vector points toward the sun: a positive Z component is required for
// any light to reach the horizontal ground (Z = 0) and block top faces, so a
// zero or horizontal sun produces an empty mask. The mask marks every pixel
// covered by at least one shadow with full alpha (255); shadows overlapping on
// the same pixel are merged, so compositing the mask never darkens a pixel
// twice. Ground shadows are hidden by blocks standing in front of them and
// shadows landing on a block are clipped to that block's visible top face.
//
// The origin is the same projection origin used by Blocks.Draw, so the mask
// aligns exactly with the drawn blocks.
func (blocks Blocks) ShadowMask(bounds image.Rectangle, origin, sun Vec3) *image.Alpha {
	mask := image.NewAlpha(bounds)

	if len(blocks) == 0 || bounds.Empty() || sun.Z <= 0 || !finiteVec3(sun) || !finiteVec3(origin) {
		return mask
	}

	sbs := prepareShadowBlocks(blocks, origin, sun)
	if len(sbs) == 0 {
		return mask
	}

	order := shadowDrawOrder(sbs)
	owner := shadowOwners(bounds, sbs, order)

	// Ground shadows: cast toward Z = 0, keeping only pixels that are not
	// covered by a block in front of the ground.
	for i := range sbs {
		poly := shadowHull(sbs[i], 0)
		if len(poly) < 3 {
			continue
		}

		rasterShadow(poly, bounds, func(x, y int) {
			if owner[y-bounds.Min.Y][x-bounds.Min.X] < 0 {
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		})
	}

	// Rooftop shadows: each block is a receiver at its top face height. A
	// shadow pixel is kept only where its frontmost block is the receiver,
	// which clips the shadow to the visible top face and removes both the
	// caster's own footprint and anything hidden behind other blocks.
	for r := range sbs {
		topZ := sbs[r].max.Z
		top := sbs[r].top

		for c := range sbs {
			if c == r || sbs[c].max.Z <= topZ || !xyBoxesOverlap(sbs[c].reach, sbs[r].footprint) {
				continue
			}

			poly := shadowHull(sbs[c], topZ)
			if len(poly) < 3 {
				continue
			}

			receiver := r

			rasterShadow(poly, bounds, func(x, y int) {
				if owner[y-bounds.Min.Y][x-bounds.Min.X] == receiver && IV(x, y).In(top) {
					mask.SetAlpha(x, y, color.Alpha{A: 255})
				}
			})
		}
	}

	return mask
}

// DrawShadow computes the shadow mask for the sun direction and composites it
// over dst using c (or BlockShadowColor when c is nil). The alpha of c sets
// the shadow strength; overlapping shadows are merged in the mask so they are
// not darkened twice. The blocks themselves are not redrawn — call this after
// Blocks.Draw.
func (blocks Blocks) DrawShadow(dst draw.Image, origin, sun Vec3, c color.Color) {
	if c == nil {
		c = BlockShadowColor
	}

	mask := blocks.ShadowMask(dst.Bounds(), origin, sun)

	draw.DrawMask(dst, dst.Bounds(), NewUniform(c), ZP, mask, ZP, draw.Over)
}

// shadowBlock holds the precomputed world and screen geometry for one block.
type shadowBlock struct {
	block       Block
	origin      Vec3
	sun         Vec3
	max         Vec3
	footprint   Box // world XY extent at Z = 0
	reach       Box // world XY extent swept by the ground shadow
	top         Polygon
	silhouette  Polygon
}

// prepareShadowBlocks drops degenerate blocks (zero volume or non-finite
// geometry) and precomputes the geometry used by every shadow pass.
func prepareShadowBlocks(blocks Blocks, origin, sun Vec3) []shadowBlock {
	sbs := make([]shadowBlock, 0, len(blocks))

	for _, b := range blocks {
		p, s := b.Pos, b.Size

		if s.X <= 0 || s.Y <= 0 || s.Z <= 0 || !finiteVec3(p) || !finiteVec3(s) {
			continue
		}

		max := p.Add(s)

		sb := shadowBlock{
			block:     b,
			origin:    origin,
			sun:       sun,
			max:       max,
			footprint: B(p.X, p.Y, 0, max.X, max.Y, 0),
			reach:     shadowReachBox(p, max, sun),
		}

		corners := b.Corners(origin)
		sb.top = clampPolygon(corners.Top())
		sb.silhouette = clampPolygon(corners.Shape())

		sbs = append(sbs, sb)
	}

	return sbs
}

// shadowDrawOrder returns block indices in back-to-front draw order, matching
// Block.Behind so the last entry is the frontmost block at every pixel.
func shadowDrawOrder(sbs []shadowBlock) []int {
	order := make([]int, len(sbs))

	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(i, j int) bool {
		return sbs[order[i]].block.Behind(sbs[order[j]].block)
	})

	return order
}

// shadowOwners returns a grid indexed [y][x] over bounds holding the index of
// the frontmost block covering each pixel, or -1 for background pixels.
func shadowOwners(bounds image.Rectangle, sbs []shadowBlock, order []int) [][]int {
	owner := make([][]int, bounds.Dy())

	for y := range owner {
		row := make([]int, bounds.Dx())

		for x := range row {
			row[x] = -1
		}

		owner[y] = row
	}

	for _, i := range order {
		rasterShadow(sbs[i].silhouette, bounds, func(x, y int) {
			owner[y-bounds.Min.Y][x-bounds.Min.X] = i
		})
	}

	return owner
}

// rasterShadow calls fn for every pixel of bounds inside the polygon. Its
// cost is bounded by the polygon's clipped bounding box, so polygons that
// extend far outside the viewport only iterate the visible intersection.
func rasterShadow(p Polygon, bounds image.Rectangle, fn func(x, y int)) {
	if len(p) < 3 {
		return
	}

	b := p.Bounds().Intersect(bounds)

	for x := b.Min.X; x < b.Max.X; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if IV(x, y).In(p) {
				fn(x, y)
			}
		}
	}
}

// shadowHull projects the part of the block at or above the horizontal plane
// at planeZ onto that plane and returns the convex hull of the projected
// corners in screen space. A block entirely below the plane cannot shadow it,
// which yields nil. Corners below the plane are first raised onto it (the
// block's cross-section at the plane), so the hull is exactly the silhouette
// of the block's shadow volume where it crosses the plane.
func shadowHull(sb shadowBlock, planeZ float64) Polygon {
	if sb.max.Z <= planeZ {
		return nil
	}

	space := sb.block.Space()

	corners := []Vec3{
		space.LeftUp, space.LeftDown, space.FrontDown, space.RightDown,
		space.RightUp, space.BackUp, space.BackDown, space.FrontUp,
	}

	points := make([]Vec, 0, len(corners))

	for _, c := range corners {
		// Raise corners below the plane onto it: the shadow of the part of
		// the block above the plane starts at this cross-section.
		if c.Z < planeZ {
			c.Z = planeZ
		}

		points = append(points, clampVec(blockCorner(projectToPlane(c, planeZ, sb.sun), sb.origin)))
	}

	return convexHull(points)
}

// xyBoxesOverlap reports whether two boxes overlap in the ground plane.
// Touching edges count as overlapping so a shadow that ends exactly on a
// block edge is not pruned away; unlike Box.Overlaps it ignores Z, which is
// zero for both footprint and reach boxes anyway.
func xyBoxesOverlap(a, b Box) bool {
	return a.Min.X <= b.Max.X && b.Min.X <= a.Max.X &&
		a.Min.Y <= b.Max.Y && b.Min.Y <= a.Max.Y
}

// projectToPlane intersects the ray starting at p and traveling away from the
// sun with the horizontal plane at planeZ. The caller guarantees sun.Z > 0.
// A negative t means p is below the plane; the corner is then kept in place
// (t = 0) by the clamp.
func projectToPlane(p Vec3, planeZ float64, sun Vec3) Vec3 {
	t := (p.Z - planeZ) / sun.Z

	if t < 0 {
		t = 0
	}

	return p.Sub(sun.Scaled(t))
}

// shadowReachBox returns the world XY box swept by the block's ground shadow:
// the union of its footprint and the footprint shifted along the ground ray
// from the block's top.
func shadowReachBox(min, max, sun Vec3) Box {
	// Horizontal distance the top corners travel before reaching Z = 0.
	// Light travels away from the sun, so the shift is -sun scaled.
	dx, dy := -sun.X*(max.Z/sun.Z), -sun.Y*(max.Z/sun.Z)

	x0, x1 := min.X, max.X+dx
	y0, y1 := min.Y, max.Y+dy

	if x1 < x0 {
		x0, x1 = x1, x0
	}

	if y1 < y0 {
		y0, y1 = y1, y0
	}

	return B(x0, y0, 0, x1, y1, 0)
}

// convexHull returns the convex hull of the given screen points using
// Andrew's monotone chain. Collinear points on an edge are removed and
// duplicate points are ignored; fewer than three unique points yield nil.
func convexHull(points []Vec) Polygon {
	if len(points) < 3 {
		return nil
	}

	pts := make([]Vec, len(points))
	copy(pts, points)
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].X != pts[j].X {
			return pts[i].X < pts[j].X
		}

		return pts[i].Y < pts[j].Y
	})

	unique := pts[:0]

	for _, p := range pts {
		if len(unique) == 0 || unique[len(unique)-1] != p {
			unique = append(unique, p)
		}
	}

	if len(unique) < 3 {
		return nil
	}

	cross := func(o, a, b Vec) float64 {
		return a.Sub(o).Cross(b.Sub(o))
	}

	lower := make(Polygon, 0, len(unique))

	for _, p := range unique {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], p) <= 0 {
			lower = lower[:len(lower)-1]
		}

		lower = append(lower, p)
	}

	upper := make(Polygon, 0, len(unique))

	for i := len(unique) - 1; i >= 0; i-- {
		p := unique[i]

		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], p) <= 0 {
			upper = upper[:len(upper)-1]
		}

		upper = append(upper, p)
	}

	hull := append(lower[:len(lower)-1], upper[:len(upper)-1]...)

	// All input points were collinear: the hull encloses no area and cannot
	// be rasterized as a shadow polygon.
	if len(hull) < 3 {
		return nil
	}

	return hull
}

// clampVec keeps a screen coordinate finite and within shadowCoordLimit.
func clampVec(v Vec) Vec {
	return Vec{
		X: clampFloat(v.X),
		Y: clampFloat(v.Y),
	}
}

// clampPolygon applies clampVec to every vertex of the polygon.
func clampPolygon(p Polygon) Polygon {
	out := make(Polygon, len(p))

	for i, v := range p {
		out[i] = clampVec(v)
	}

	return out
}

func clampFloat(f float64) float64 {
	if math.IsNaN(f) {
		return shadowCoordLimit
	}

	if f > shadowCoordLimit {
		return shadowCoordLimit
	}

	if f < -shadowCoordLimit {
		return -shadowCoordLimit
	}

	return f
}

func finiteVec3(v Vec3) bool {
	return !math.IsInf(v.X, 0) && !math.IsInf(v.Y, 0) && !math.IsInf(v.Z, 0) &&
		!math.IsNaN(v.X) && !math.IsNaN(v.Y) && !math.IsNaN(v.Z)
}
