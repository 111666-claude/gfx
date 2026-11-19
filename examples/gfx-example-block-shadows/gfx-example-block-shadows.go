package main

import "github.com/peterhellberg/gfx"

func main() {
	var (
		dst    = gfx.NewImage(720, 720, gfx.ColorNRGBA(235, 235, 235, 255))
		origin = gfx.V3(360, -360, 2.2)
		// The sun vector points toward the sun. Its positive Z component is
		// what lets the light strike the ground and the block top faces.
		sun = gfx.V3(-1, 0, 1)

		blocks = gfx.Blocks{
			// A tall wall and a lower wall standing next to it.
			gfx.NewBlock(gfx.V3(0, 0, 0), gfx.V3(12, 12, 34), gfx.BlockColorBlue),
			gfx.NewBlock(gfx.V3(24, 0, 0), gfx.V3(12, 12, 12), gfx.BlockColorRed),
		}
	)

	// Draw the blocks first, then composite their shadows on top. The shadow
	// of the tall wall lands on the low wall's top face and the open ground
	// beyond it; overlapping shadows are merged instead of darkening twice.
	blocks.Sort()
	blocks.Draw(dst, origin)
	blocks.DrawShadow(dst, origin, sun, gfx.ColorNRGBA(20, 20, 30, 140))

	gfx.SavePNG("gfx-example-block-shadows.png", dst)
}
