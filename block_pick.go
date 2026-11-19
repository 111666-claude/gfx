package gfx

import "math"

// BlockFace identifies one of the three faces of a Block that are visible in
// the isometric projection used by Block.Draw.
type BlockFace int

const (
	// BlockFaceNone is the zero value, used when no block was picked.
	BlockFaceNone BlockFace = iota

	// BlockFaceTop is the face at Pos.Z + Size.Z.
	BlockFaceTop

	// BlockFaceLeft is the face at Pos.X. (the face Block.Draw colors Dark)
	BlockFaceLeft

	// BlockFaceRight is the face at Pos.Y. (the face Block.Draw colors Medium)
	BlockFaceRight
)

// String returns the name of the face.
func (f BlockFace) String() string {
	switch f {
	case BlockFaceTop:
		return "top"
	case BlockFaceLeft:
		return "left"
	case BlockFaceRight:
		return "right"
	default:
		return "none"
	}
}

// BlockHit is the result of Blocks.Pick.
type BlockHit struct {
	// Index is the index into the Blocks slice that was picked,
	// or -1 if no block was picked.
	Index int

	// Face is the visible face at the picked screen coordinate.
	Face BlockFace

	// Depth is the distance of Point along the isometric view direction,
	// increasing away from the viewer. Blocks drawn in front of other
	// blocks have a smaller Depth than the blocks they cover.
	//
	// Only differences between depths are meaningful: the zero point is
	// the plane X+Y-Z = 0 in block space.
	Depth float64

	// Point is the picked point on the face, in the same 3D space as
	// Block.Pos and Block.Size.
	Point Vec3
}

// blockTriangleFaces maps each triangle returned by Block.Triangles to the
// face it belongs to.
var blockTriangleFaces = [...]BlockFace{
	BlockFaceLeft, BlockFaceLeft,
	BlockFaceRight, BlockFaceRight,
	BlockFaceTop, BlockFaceTop,
}

// Pick returns the block and face at the given screen coordinate and
// projection origin.
//
// Picking follows the same order as Blocks.Draw: blocks are visited from the
// end of the slice towards the start, and the triangles of each block from
// the last one towards the first. The first triangle that contains the screen
// coordinate wins, which means the returned face is exactly the one that
// painted that pixel. A slice sorted back-to-front with Blocks.Sort therefore
// picks the geometrically nearest block, and an unsorted slice picks the block
// that covers the others when drawn, even if it is not the nearest.
//
// The screen coordinate is tested against the same floating point coordinates
// as Triangle.Contains uses when drawing, so the picked face is the face the
// pixel was drawn with, and a pixel that Block.Draw left untouched is never
// picked. Pixels on an edge or corner shared by several triangles go to the
// last triangle drawn there, making the result stable and independent of
// iteration order.
//
// A block is not pickable if its Pos or Size contains NaN or an infinity, or
// if it has a negative Size component. Blocks with a negative Size are
// skipped by Block.Draw (its triangles are wound the other way round), so they
// can not be seen or picked.
//
// If p or origin contains NaN or an infinity, or origin.Z is not positive,
// nothing is picked. A non-positive origin.Z either collapses every block to
// a single point or mirrors the projection, and in both cases the Rect used by
// Block.Rect (and therefore Blocks.Draw) ends up empty, so nothing is drawn.
// Degenerate faces that paint no pixels, such as the top of a block with
// Size.Z == 0, are never returned.
//
// When no block is picked, Index is -1 and ok is false.
//
// Pick does not modify blocks.
func (blocks Blocks) Pick(p Vec, origin Vec3) (hit BlockHit, ok bool) {
	if !isFiniteVec(p) || !isFiniteVec3(origin) || origin.Z <= 0 {
		return BlockHit{Index: -1}, false
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		if h, picked := blocks[i].pick(p, origin); picked {
			h.Index = i

			return h, true
		}
	}

	return BlockHit{Index: -1}, false
}

// pick returns the visible face of the block at p. Index is left at -1.
func (b Block) pick(p Vec, origin Vec3) (BlockHit, bool) {
	if !isFiniteVec3(b.Pos) || !isFiniteVec3(b.Size) || b.Size.X < 0 || b.Size.Y < 0 || b.Size.Z < 0 {
		return BlockHit{Index: -1}, false
	}

	// Every triangle of the block fits inside its screen Rect, so a point
	// outside it can not hit the block.
	if r := b.Rect(origin); p.X < r.Min.X || p.X > r.Max.X || p.Y < r.Min.Y || p.Y > r.Max.Y {
		return BlockHit{Index: -1}, false
	}

	triangles := b.Triangles(origin)

	for i := len(triangles) - 1; i >= 0; i-- {
		if !triangles[i].Contains(p) {
			continue
		}

		face := blockTriangleFaces[i]

		point, valid := b.facePoint(face, p, origin)
		if !valid {
			continue
		}

		depth := blockDepth(point)
		if !isFinite(depth) {
			continue
		}

		return BlockHit{Index: -1, Face: face, Depth: depth, Point: point}, true
	}

	return BlockHit{Index: -1}, false
}

// facePoint returns the point on the given face that projects to the screen
// coordinate p.
//
// blockCorner projects a point with:
//
//	screenX =  (X-Y) * sqrt(3)/2 * origin.Z + origin.X
//	screenY = -((X+Y)/2 + Z) * origin.Z - origin.Y
//
// so inverting the projection for the plane of a face gives a single point.
func (b Block) facePoint(face BlockFace, p Vec, origin Vec3) (Vec3, bool) {
	var (
		h = (p.X - origin.X) / origin.Z  // (X-Y) * sqrt(3)/2
		v = -(p.Y + origin.Y) / origin.Z // (X+Y)/2 + Z

		// d is X-Y and s is X+Y+2Z. Neither depends on the face.
		d = h * 2 / math.Sqrt(3)
		s = 2 * v
	)

	var point Vec3

	switch face {
	case BlockFaceTop:
		z := b.Pos.Z + b.Size.Z
		sum := s - 2*z

		point = V3((sum+d)/2, (sum-d)/2, z)
	case BlockFaceLeft:
		x := b.Pos.X

		point = V3(x, x-d, (s-2*x+d)/2)
	case BlockFaceRight:
		y := b.Pos.Y

		point = V3(y+d, y, (s-2*y-d)/2)
	default:
		return Vec3{}, false
	}

	if !isFiniteVec3(point) {
		return Vec3{}, false
	}

	return point, true
}

// blockViewDirection is the unit vector the isometric projection looks along.
// Points on the same view ray differ by a multiple of (1, 1, -1).
var blockViewDirection = V3(1, 1, -1).Unit()

// blockDepth returns the distance of point along the view direction.
func blockDepth(point Vec3) float64 {
	return point.Dot(blockViewDirection)
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func isFiniteVec(u Vec) bool {
	return isFinite(u.X) && isFinite(u.Y)
}

func isFiniteVec3(u Vec3) bool {
	return isFinite(u.X) && isFinite(u.Y) && isFinite(u.Z)
}
