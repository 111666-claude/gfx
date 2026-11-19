package gfx

import "io"

// NewBlockMesh builds an indexed triangle mesh from the given blocks.
//
// It is shorthand for blocks.Mesh(options...).
func NewBlockMesh(blocks Blocks, options ...MeshOption) (*Mesh, error) {
	return blocks.Mesh(options...)
}

// WriteOBJ builds a mesh from the blocks and writes it as an OBJ file.
//
// The mtl argument is written after mtllib; pass an empty string to omit
// the mtllib line. Use Mesh.WriteMTL to write the matching material file.
func (blocks Blocks) WriteOBJ(w io.Writer, mtl string, options ...MeshOption) error {
	m, err := blocks.Mesh(options...)
	if err != nil {
		return err
	}

	return m.WriteOBJ(w, mtl)
}

// ExportOBJ builds a mesh from the blocks and saves it to objPath, with
// its materials saved to mtlPath.
func (blocks Blocks) ExportOBJ(objPath, mtlPath string, options ...MeshOption) error {
	m, err := blocks.Mesh(options...)
	if err != nil {
		return err
	}

	return m.SaveOBJ(objPath, mtlPath)
}
