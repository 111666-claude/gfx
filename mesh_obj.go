package gfx

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// WriteMTL writes the mesh materials as an MTL file.
func (m *Mesh) WriteMTL(w io.Writer) error {
	if err := m.validate(); err != nil {
		return err
	}

	bw := bufio.NewWriter(w)

	if _, err := fmt.Fprintln(bw, "# gfx block mesh materials"); err != nil {
		return err
	}

	for _, mat := range m.Materials {
		c := mat.Color

		if _, err := fmt.Fprintf(bw, "\nnewmtl %s\n", mat.Name); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(bw, "Kd %s %s %s\n",
			meshFormatFloat(float64(c.R)/255),
			meshFormatFloat(float64(c.G)/255),
			meshFormatFloat(float64(c.B)/255),
		); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(bw, "d %s\n", meshFormatFloat(float64(c.A)/255)); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// WriteOBJ writes the mesh as an OBJ file.
//
// The mtl argument is the file name written after mtllib; pass an empty
// string to omit the mtllib line. Faces are written as triangles, each
// referring to its vertex normal and its material (usemtl).
func (m *Mesh) WriteOBJ(w io.Writer, mtl string) error {
	if err := m.validate(); err != nil {
		return err
	}

	bw := bufio.NewWriter(w)

	if _, err := fmt.Fprintln(bw, "# gfx block mesh"); err != nil {
		return err
	}

	if mtl != "" {
		if _, err := fmt.Fprintf(bw, "mtllib %s\n", mtl); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(bw, "o mesh"); err != nil {
		return err
	}

	for _, p := range m.Positions {
		if _, err := fmt.Fprintf(bw, "v %s %s %s\n",
			meshFormatFloat(p.X), meshFormatFloat(p.Y), meshFormatFloat(p.Z)); err != nil {
			return err
		}
	}

	for _, n := range m.Normals {
		if _, err := fmt.Fprintf(bw, "vn %s %s %s\n",
			meshFormatFloat(n.X), meshFormatFloat(n.Y), meshFormatFloat(n.Z)); err != nil {
			return err
		}
	}

	material := -1

	for _, t := range m.Triangles {
		if t.Material != material {
			material = t.Material

			if _, err := fmt.Fprintf(bw, "usemtl %s\n", m.Materials[material].Name); err != nil {
				return err
			}
		}

		// OBJ indices are 1-based.
		if _, err := fmt.Fprintf(bw, "f %d//%d %d//%d %d//%d\n",
			t.Vertices[0]+1, t.Normal+1,
			t.Vertices[1]+1, t.Normal+1,
			t.Vertices[2]+1, t.Normal+1); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// SaveOBJ writes the mesh to objPath and its materials to mtlPath.
//
// The OBJ file refers to the MTL file by its base name, as OBJ paths are
// relative to the OBJ file.
func (m *Mesh) SaveOBJ(objPath, mtlPath string) error {
	if err := m.validate(); err != nil {
		return err
	}

	f, err := os.Create(mtlPath)
	if err != nil {
		return fmt.Errorf("gfx: write MTL: %w", err)
	}

	if err := m.WriteMTL(f); err != nil {
		f.Close()
		return fmt.Errorf("gfx: write MTL: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("gfx: write MTL: %w", err)
	}

	f, err = os.Create(objPath)
	if err != nil {
		return fmt.Errorf("gfx: write OBJ: %w", err)
	}

	if err := m.WriteOBJ(f, filepath.Base(mtlPath)); err != nil {
		f.Close()
		return fmt.Errorf("gfx: write OBJ: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("gfx: write OBJ: %w", err)
	}

	return nil
}

// validate checks that the indexes of m are consistent.
func (m *Mesh) validate() error {
	if m == nil {
		return fmt.Errorf("%w: nil mesh", ErrMeshInvalid)
	}

	for i, f := range m.Faces {
		for _, v := range f.Vertices {
			if v < 0 || v >= len(m.Positions) {
				return fmt.Errorf("%w: face %d refers to position %d", ErrMeshInvalid, i, v)
			}
		}

		if f.Normal < 0 || f.Normal >= len(m.Normals) {
			return fmt.Errorf("%w: face %d refers to normal %d", ErrMeshInvalid, i, f.Normal)
		}

		if f.Material < 0 || f.Material >= len(m.Materials) {
			return fmt.Errorf("%w: face %d refers to material %d", ErrMeshInvalid, i, f.Material)
		}
	}

	for i, t := range m.Triangles {
		for _, v := range t.Vertices {
			if v < 0 || v >= len(m.Positions) {
				return fmt.Errorf("%w: triangle %d refers to position %d", ErrMeshInvalid, i, v)
			}
		}

		if t.Normal < 0 || t.Normal >= len(m.Normals) {
			return fmt.Errorf("%w: triangle %d refers to normal %d", ErrMeshInvalid, i, t.Normal)
		}

		if t.Material < 0 || t.Material >= len(m.Materials) {
			return fmt.Errorf("%w: triangle %d refers to material %d", ErrMeshInvalid, i, t.Material)
		}

		if t.Face < 0 || t.Face >= len(m.Faces) {
			return fmt.Errorf("%w: triangle %d refers to face %d", ErrMeshInvalid, i, t.Face)
		}
	}

	return nil
}

// meshFormatFloat formats a float without exponent or unnecessary zeros.
func meshFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
