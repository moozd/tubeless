package render

import "github.com/go-gl/gl/v3.3-core/gl"

// newFullscreenQuadVAO builds a VAO for the fullscreen triangle-strip quad
// used by every post-process pass (see fullscreen.vert): two triangles
// covering clip space, so the fragment shader runs once per output pixel.
func newFullscreenQuadVAO() uint32 {
	verts := []float32{-1, -1, 1, -1, -1, 1, 1, -1, 1, 1, -1, 1}
	var vbo, vao uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.STATIC_DRAW)
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.BindVertexArray(0)
	return vao
}
