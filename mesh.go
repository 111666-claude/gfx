package gfx

import (
	"fmt"
	"image/color"
	"math"
	"sort"
)

// Mesh errors. They are returned (wrapped) by Blocks.Mesh and the mesh
// writers; use errors.Is to test for a specific kind.
var (
	// ErrMeshNonFinite is returned when a block position or size contains
	// NaN or an infinity.
	ErrMeshNonFinite = Error("gfx: block mesh: non-finite coordinate")

	// ErrMeshNotGridAligned is returned when a block position or size is
	// not an integer.
	ErrMeshNotGridAligned = Error("gfx: block mesh: block is not aligned to the integer grid")

	// ErrMeshDegenerateBlock is returned when a block has a non-positive
	// size on any axis.
	ErrMeshDegenerateBlock = Error("gfx: block mesh: degenerate block")

	// ErrMeshCoordinateRange is returned when a block coordinate is too
	// large to be represented exactly (or added to another size) as an
	// integer.
	ErrMeshCoordinateRange = Error("gfx: block mesh: coordinate out of supported range")

	// ErrMeshOverlap is returned when two blocks overlap with a positive
	// volume. Blocks touching along a face, edge or corner are allowed.
	ErrMeshOverlap = Error("gfx: block mesh: blocks partially overlap")

	// ErrMeshFaceLimit is returned when the mesh would contain more faces
	// than the configured limit.
	ErrMeshFaceLimit = Error("gfx: block mesh: face limit exceeded")

	// ErrMeshInvalid is returned by the mesh writers when a Mesh is
	// malformed.
	ErrMeshInvalid = Error("gfx: block mesh: invalid mesh")
)

// DefaultMaxMeshFaces is the default upper limit on the number of faces
// emitted by Blocks.Mesh.
const DefaultMaxMeshFaces = 1000000

// maxMeshCoordinate is the largest magnitude accepted for a block
// coordinate. Keeping the coordinate and the coordinate plus the size
// within 2^52 makes every value exactly representable as a float64 and
// safely convertible to int64.
const maxMeshCoordinate = int64(1) << 52

// MeshMaterial is a surface material referenced by the faces of a Mesh.
//
// Materials are created from the color of the exterior faces: top (+Z)
// faces use BlockColor.Light, bottom (-Z) and left (-X) faces use
// BlockColor.Dark, and the remaining faces use BlockColor.Medium. This
// matches the shading used by Block.Draw.
type MeshMaterial struct {
	Name  string
	Color color.NRGBA
}

// MeshFace is a planar quad of a Mesh.
//
// Vertices indexes into Mesh.Positions in counter-clockwise order as seen
// from outside the mesh, Normal indexes Mesh.Normals, Material indexes
// Mesh.Materials, and Color equals the material color.
//
// A face that is only partially hidden by a neighbouring block is split
// into its exposed rectangles, so every face is fully visible.
type MeshFace struct {
	Vertices [4]int
	Normal   int
	Material int
	Color    color.NRGBA
}

// MeshTriangle is a triangle of a Mesh.
//
// Vertices indexes Mesh.Positions in counter-clockwise order as seen from
// outside the mesh, Normal indexes Mesh.Normals, Material indexes
// Mesh.Materials, and Face indexes the MeshFace the triangle belongs to.
type MeshTriangle struct {
	Vertices [3]int
	Normal   int
	Material int
	Face     int
}

// Mesh is an indexed triangle mesh built from grid-aligned Blocks.
//
// Every face contributes four vertices and two triangles. Positions,
// normals, materials, faces and triangles are ordered deterministically,
// independent of the order the blocks were passed in to Blocks.Mesh.
type Mesh struct {
	// Positions contains four positions per face.
	Positions []Vec3

	// Normals contains the distinct axis-aligned normals used by Faces.
	Normals []Vec3

	// Faces contains the exterior quads of the mesh.
	Faces []MeshFace

	// Triangles contains the triangulation of Faces.
	Triangles []MeshTriangle

	// Materials contains the materials used by Faces, in first-use order.
	Materials []MeshMaterial
}

// Area returns the total surface area of the mesh faces.
func (m *Mesh) Area() float64 {
	if m == nil {
		return 0
	}

	var area float64

	for _, f := range m.Faces {
		a := m.Positions[f.Vertices[0]]
		b := m.Positions[f.Vertices[1]]
		c := m.Positions[f.Vertices[2]]
		d := m.Positions[f.Vertices[3]]

		area += meshTriangleArea(a, b, c) + meshTriangleArea(a, c, d)
	}

	return area
}

// MeshOption configures Blocks.Mesh.
type MeshOption func(*meshOptions)

type meshOptions struct {
	merge    bool
	maxFaces int
}

// WithMeshMerge enables merging of exterior faces that are coplanar, have
// the same normal and the same color. Merged faces are emitted as maximal
// rectangles.
func WithMeshMerge() MeshOption {
	return func(o *meshOptions) {
		o.merge = true
	}
}

// WithMeshMaxFaces sets the upper limit on the number of faces emitted by
// Blocks.Mesh. The limit is applied after optional face merging, and is
// ignored when n is less than or equal to zero.
func WithMeshMaxFaces(n int) MeshOption {
	return func(o *meshOptions) {
		if n > 0 {
			o.maxFaces = n
		}
	}
}

// Mesh builds an indexed triangle mesh from the blocks.
//
// Blocks must be aligned to the integer grid: Pos and Size must have
// integer components, Size must be positive on every axis, and all
// coordinates must be finite and within a range that can be represented
// exactly. Negative positions and different integer sizes are supported.
// Two blocks may touch along a face, edge or corner, but must not overlap
// with a positive volume; such an overlap is reported as ErrMeshOverlap.
// Non-finite, degenerate, non-integer and out-of-range blocks are reported
// with the corresponding sentinel errors.
//
// A face is omitted when it is completely covered by a touching block; a
// partially covered face is split and only its exposed part is emitted.
// With WithMeshMerge, exposed coplanar, same-colored faces are combined
// into maximal rectangles (after clipping, so a merged quad never includes
// hidden geometry).
//
// The output is canonical: faces are sorted by axis, plane, direction,
// position and color, so permuting the input blocks produces the same
// mesh, indices and OBJ bytes.
func (blocks Blocks) Mesh(options ...MeshOption) (*Mesh, error) {
	opts := meshOptions{maxFaces: DefaultMaxMeshFaces}

	for _, opt := range options {
		if opt != nil {
			opt(&opts)
		}
	}

	bs := make([]meshBlock, len(blocks))
	for i, b := range blocks {
		mb, err := meshBlockFromBlock(i, b)
		if err != nil {
			return nil, err
		}

		bs[i] = mb
	}

	if err := meshCheckOverlaps(bs); err != nil {
		return nil, err
	}

	rects := meshExposedRects(bs)

	if opts.merge {
		rects = meshMergeRects(rects)
	}

	if opts.maxFaces > 0 && len(rects) > opts.maxFaces {
		return nil, fmt.Errorf("%w: %d faces exceed the limit of %d",
			ErrMeshFaceLimit, len(rects), opts.maxFaces)
	}

	return newMesh(rects), nil
}

// meshBlock is a validated block with integer grid coordinates.
type meshBlock struct {
	min   [3]int64
	max   [3]int64
	color BlockColor
}

// String returns the block extent, useful in error messages.
func (b meshBlock) String() string {
	return fmt.Sprintf("[%d,%d]x[%d,%d]x[%d,%d]",
		b.min[0], b.max[0],
		b.min[1], b.max[1],
		b.min[2], b.max[2])
}

// overlaps reports whether the interiors of b and a overlap.
func (b meshBlock) overlaps(a meshBlock) bool {
	for axis := range 3 {
		if b.min[axis] >= a.max[axis] || a.min[axis] >= b.max[axis] {
			return false
		}
	}

	return true
}

func meshBlockFromBlock(i int, b Block) (meshBlock, error) {
	var (
		mb   meshBlock
		pos  = [3]float64{b.Pos.X, b.Pos.Y, b.Pos.Z}
		size = [3]float64{b.Size.X, b.Size.Y, b.Size.Z}
		axes = [3]string{"X", "Y", "Z"}
	)

	for axis := range 3 {
		if !meshFinite(pos[axis]) || !meshFinite(size[axis]) {
			return mb, fmt.Errorf("%w: block %d has a non-finite %s position or size",
				ErrMeshNonFinite, i, axes[axis])
		}

		if size[axis] <= 0 {
			return mb, fmt.Errorf("%w: block %d has %s size %v",
				ErrMeshDegenerateBlock, i, axes[axis], size[axis])
		}

		if pos[axis] != math.Trunc(pos[axis]) || size[axis] != math.Trunc(size[axis]) {
			return mb, fmt.Errorf("%w: block %d has a non-integer %s position or size",
				ErrMeshNotGridAligned, i, axes[axis])
		}

		high := pos[axis] + size[axis]

		if !meshInRange(pos[axis]) || !meshInRange(high) {
			return mb, fmt.Errorf("%w: block %d has %s coordinates %v..%v",
				ErrMeshCoordinateRange, i, axes[axis], pos[axis], high)
		}

		mb.min[axis] = int64(pos[axis])
		mb.max[axis] = int64(high)
	}

	mb.color = b.Color

	return mb, nil
}

func meshFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func meshInRange(v float64) bool {
	return v >= -float64(maxMeshCoordinate) && v <= float64(maxMeshCoordinate)
}

// meshCheckOverlaps returns ErrMeshOverlap for the first pair of blocks
// with overlapping interiors. The sweep along X keeps the check cheap for
// spatially ordered input.
func meshCheckOverlaps(bs []meshBlock) error {
	order := make([]int, len(bs))
	for i := range order {
		order[i] = i
	}

	sort.Slice(order, func(i, j int) bool {
		if bs[order[i]].min[0] != bs[order[j]].min[0] {
			return bs[order[i]].min[0] < bs[order[j]].min[0]
		}

		return order[i] < order[j]
	})

	for oi, i := range order {
		for _, j := range order[oi+1:] {
			if bs[j].min[0] >= bs[i].max[0] {
				break
			}

			if bs[i].overlaps(bs[j]) {
				return fmt.Errorf("%w: block %d %v and block %d %v",
					ErrMeshOverlap, i, bs[i], j, bs[j])
			}
		}
	}

	return nil
}

// meshPlaneKey identifies the blocks starting or ending on a plane.
type meshPlaneKey struct {
	axis  int
	plane int64
}

// meshAdjacency indexes blocks by the planes they start and end on. A
// face on the +side of its block is only covered by blocks starting at the
// same plane, and a face on the -side only by blocks ending there.
type meshAdjacency struct {
	blocks []meshBlock
	atMin  map[meshPlaneKey][]int
	atMax  map[meshPlaneKey][]int
}

func newMeshAdjacency(bs []meshBlock) meshAdjacency {
	adj := meshAdjacency{
		blocks: bs,
		atMin:  make(map[meshPlaneKey][]int, len(bs)*3),
		atMax:  make(map[meshPlaneKey][]int, len(bs)*3),
	}

	for i, b := range bs {
		for axis := range 3 {
			minKey := meshPlaneKey{axis, b.min[axis]}
			maxKey := meshPlaneKey{axis, b.max[axis]}

			adj.atMin[minKey] = append(adj.atMin[minKey], i)
			adj.atMax[maxKey] = append(adj.atMax[maxKey], i)
		}
	}

	return adj
}

// meshFaceUV maps a face axis to the two axes used for the face's u and v
// coordinates. The mapping is chosen so that +u cross +v points along the
// positive direction of the face axis.
var meshFaceUV = [3][2]int{
	{1, 2}, // X: u=Y, v=Z
	{2, 0}, // Y: u=Z, v=X
	{0, 1}, // Z: u=X, v=Y
}

// meshRect is an axis-aligned rectangle on a face plane.
type meshRect struct {
	axis   int
	side   int // +1 or -1, the direction the face points in
	plane  int64
	u0, u1 int64
	v0, v1 int64
	color  color.NRGBA
}

// meshFaceColor returns the color used for a block face with the given
// axis and outward direction.
func meshFaceColor(c BlockColor, axis, side int) color.NRGBA {
	switch {
	case axis == 2 && side > 0:
		return c.Light
	case axis == 2 && side < 0:
		return c.Dark
	case axis == 0 && side < 0:
		return c.Dark
	default:
		return c.Medium
	}
}

// meshBlockFace returns the rectangle for one face of b.
func meshBlockFace(b meshBlock, axis, side int) meshRect {
	uAxis, vAxis := meshFaceUV[axis][0], meshFaceUV[axis][1]

	plane := b.min[axis]
	if side > 0 {
		plane = b.max[axis]
	}

	return meshRect{
		axis:  axis,
		side:  side,
		plane: plane,
		u0:    b.min[uAxis],
		u1:    b.max[uAxis],
		v0:    b.min[vAxis],
		v1:    b.max[vAxis],
		color: meshFaceColor(b.color, axis, side),
	}
}

// meshCover is the projection of a touching block onto a face plane.
type meshCover struct {
	u0, u1 int64
	v0, v1 int64
}

// meshExposedRects returns the exposed parts of all block faces.
func meshExposedRects(bs []meshBlock) []meshRect {
	adj := newMeshAdjacency(bs)
	var rects []meshRect

	for i, b := range bs {
		for axis := range 3 {
			for _, side := range []int{-1, 1} {
				r := meshBlockFace(b, axis, side)
				rects = append(rects, meshExposedParts(adj, i, r)...)
			}
		}
	}

	return rects
}

// meshExposedParts splits r into the rectangles not covered by touching
// blocks. If nothing covers r, it is returned as is.
func meshExposedParts(adj meshAdjacency, self int, r meshRect) []meshRect {
	uAxis, vAxis := meshFaceUV[r.axis][0], meshFaceUV[r.axis][1]
	key := meshPlaneKey{r.axis, r.plane}
	index := adj.atMin
	if r.side < 0 {
		index = adj.atMax
	}

	candidates := index[key]

	var (
		us, vs []int64
		covers []meshCover
	)

	for _, j := range candidates {
		if j == self {
			continue
		}

		b := adj.blocks[j]
		c := meshCover{
			u0: max(r.u0, b.min[uAxis]),
			u1: min(r.u1, b.max[uAxis]),
			v0: max(r.v0, b.min[vAxis]),
			v1: min(r.v1, b.max[vAxis]),
		}

		if c.u0 >= c.u1 || c.v0 >= c.v1 {
			continue
		}

		covers = append(covers, c)
		us = append(us, c.u0, c.u1)
		vs = append(vs, c.v0, c.v1)
	}

	if len(covers) == 0 {
		return []meshRect{r}
	}

	us = meshSortUnique(append(us, r.u0, r.u1))
	vs = meshSortUnique(append(vs, r.v0, r.v1))

	nU, nV := len(us)-1, len(vs)-1
	covered := make([]bool, nU*nV)

	for _, c := range covers {
		ui0, ui1 := meshIndex(us, c.u0), meshIndex(us, c.u1)
		vi0, vi1 := meshIndex(vs, c.v0), meshIndex(vs, c.v1)

		for vi := vi0; vi < vi1; vi++ {
			for ui := ui0; ui < ui1; ui++ {
				covered[vi*nU+ui] = true
			}
		}
	}

	return meshCellsToRects(r.axis, r.side, r.plane, r.color, us, vs, covered, false)
}

// meshMergeRects merges coplanar, same-colored rectangles that share full
// edges. Every plane group is rasterized and the covered cells are split
// into greedy runs, which makes the result deterministic.
func meshMergeRects(rects []meshRect) []meshRect {
	type key struct {
		axis  int
		side  int
		plane int64
		color color.NRGBA
	}

	groups := make(map[key][]meshRect, len(rects))

	for _, r := range rects {
		k := key{r.axis, r.side, r.plane, r.color}
		groups[k] = append(groups[k], r)
	}

	merged := make([]meshRect, 0, len(rects))

	for k, group := range groups {
		var us, vs []int64

		for _, r := range group {
			us = append(us, r.u0, r.u1)
			vs = append(vs, r.v0, r.v1)
		}

		us = meshSortUnique(us)
		vs = meshSortUnique(vs)

		nU, nV := len(us)-1, len(vs)-1

		if nU <= 0 || nV <= 0 {
			continue
		}

		covered := make([]bool, nU*nV)

		for _, r := range group {
			ui0, ui1 := meshIndex(us, r.u0), meshIndex(us, r.u1)
			vi0, vi1 := meshIndex(vs, r.v0), meshIndex(vs, r.v1)

			for vi := vi0; vi < vi1; vi++ {
				for ui := ui0; ui < ui1; ui++ {
					covered[vi*nU+ui] = true
				}
			}
		}

		merged = append(merged, meshCellsToRects(k.axis, k.side, k.plane, k.color, us, vs, covered, true)...)
	}

	return merged
}

// meshCellsToRects greedily merges the cells equal to want into
// rectangles. Cells are first merged into horizontal runs, then runs with
// identical bounds are extended downwards.
func meshCellsToRects(axis, side int, plane int64, c color.NRGBA, us, vs []int64, cells []bool, want bool) []meshRect {
	nU, nV := len(us)-1, len(vs)-1

	type run struct {
		ui0, ui1 int
		vStart   int
	}

	var out []meshRect

	emit := func(r run, vEnd int) {
		out = append(out, meshRect{
			axis:  axis,
			side:  side,
			plane: plane,
			u0:    us[r.ui0],
			u1:    us[r.ui1],
			v0:    vs[r.vStart],
			v1:    vs[vEnd],
			color: c,
		})
	}

	prev := map[[2]int]run{}

	for vi := 0; vi < nV; vi++ {
		cur := map[[2]int]run{}

		for ui := 0; ui < nU; {
			if cells[vi*nU+ui] != want {
				ui++
				continue
			}

			start := ui
			for ui < nU && cells[vi*nU+ui] == want {
				ui++
			}

			k := [2]int{start, ui}
			if r, ok := prev[k]; ok {
				cur[k] = r
				delete(prev, k)
			} else {
				cur[k] = run{start, ui, vi}
			}
		}

		for _, r := range prev {
			emit(r, vi)
		}

		prev = cur
	}

	for _, r := range prev {
		emit(r, nV)
	}

	return out
}

// meshSortUnique sorts values ascending and removes duplicates.
func meshSortUnique(values []int64) []int64 {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })

	n := 0

	for _, v := range values {
		if n == 0 || values[n-1] != v {
			values[n] = v
			n++
		}
	}

	return values[:n]
}

// meshIndex returns the index of v in the sorted slice values.
func meshIndex(values []int64, v int64) int {
	return sort.Search(len(values), func(i int) bool { return values[i] >= v })
}

// meshRectLess defines the canonical ordering of mesh rectangles.
func meshRectLess(a, b meshRect) bool {
	switch {
	case a.axis != b.axis:
		return a.axis < b.axis
	case a.plane != b.plane:
		return a.plane < b.plane
	case a.side != b.side:
		return a.side < b.side
	case a.u0 != b.u0:
		return a.u0 < b.u0
	case a.v0 != b.v0:
		return a.v0 < b.v0
	case a.u1 != b.u1:
		return a.u1 < b.u1
	case a.v1 != b.v1:
		return a.v1 < b.v1
	}

	if a.color.R != b.color.R {
		return a.color.R < b.color.R
	}

	if a.color.G != b.color.G {
		return a.color.G < b.color.G
	}

	if a.color.B != b.color.B {
		return a.color.B < b.color.B
	}

	return a.color.A < b.color.A
}

// meshCorners returns the four corners of the rectangle in
// counter-clockwise order as seen from outside the mesh.
func (r meshRect) meshCorners() [4]Vec3 {
	uAxis, vAxis := meshFaceUV[r.axis][0], meshFaceUV[r.axis][1]

	at := func(u, v int64) Vec3 {
		var p [3]float64

		p[r.axis] = float64(r.plane)
		p[uAxis] = float64(u)
		p[vAxis] = float64(v)

		return Vec3{p[0], p[1], p[2]}
	}

	c0, c1 := at(r.u0, r.v0), at(r.u1, r.v0)
	c2, c3 := at(r.u1, r.v1), at(r.u0, r.v1)

	if r.side < 0 {
		return [4]Vec3{c0, c3, c2, c1}
	}

	return [4]Vec3{c0, c1, c2, c3}
}

// meshNormal returns the unit outward normal of the rectangle.
func (r meshRect) meshNormal() Vec3 {
	var n Vec3

	switch r.axis {
	case 0:
		n.X = float64(r.side)
	case 1:
		n.Y = float64(r.side)
	case 2:
		n.Z = float64(r.side)
	}

	return n
}

// newMesh builds the indexed mesh from the rectangles.
func newMesh(rects []meshRect) *Mesh {
	sort.Slice(rects, func(i, j int) bool { return meshRectLess(rects[i], rects[j]) })

	m := &Mesh{}

	// Collect the used normals first so that they get a fixed order.
	var (
		used        [3][2]bool
		normalIndex [3][2]int
	)

	for _, r := range rects {
		used[r.axis][meshSideIndex(r.side)] = true
	}

	for axis := range 3 {
		for si, side := range []int{1, -1} {
			if !used[axis][si] {
				continue
			}

			normalIndex[axis][si] = len(m.Normals)
			m.Normals = append(m.Normals, meshRect{axis: axis, side: side}.meshNormal())
		}
	}

	materialIndex := make(map[color.NRGBA]int)

	for _, r := range rects {
		mi, ok := materialIndex[r.color]
		if !ok {
			mi = len(m.Materials)
			materialIndex[r.color] = mi
			m.Materials = append(m.Materials, MeshMaterial{
				Name:  fmt.Sprintf("material_%d", mi),
				Color: r.color,
			})
		}

		ni := normalIndex[r.axis][meshSideIndex(r.side)]
		base := len(m.Positions)
		corners := r.meshCorners()

		m.Positions = append(m.Positions,
			corners[0], corners[1], corners[2], corners[3])

		face := len(m.Faces)

		m.Faces = append(m.Faces, MeshFace{
			Vertices: [4]int{base, base + 1, base + 2, base + 3},
			Normal:   ni,
			Material: mi,
			Color:    r.color,
		})

		m.Triangles = append(m.Triangles,
			MeshTriangle{
				Vertices: [3]int{base, base + 1, base + 2},
				Normal:   ni,
				Material: mi,
				Face:     face,
			},
			MeshTriangle{
				Vertices: [3]int{base, base + 2, base + 3},
				Normal:   ni,
				Material: mi,
				Face:     face,
			})
	}

	return m
}

func meshSideIndex(side int) int {
	if side < 0 {
		return 1
	}

	return 0
}

func meshTriangleArea(a, b, c Vec3) float64 {
	return meshCross(b.Sub(a), c.Sub(a)).Len() / 2
}

func meshCross(a, b Vec3) Vec3 {
	return Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}
