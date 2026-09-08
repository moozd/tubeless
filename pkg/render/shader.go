package render

import (
	"fmt"
	"strings"

	"github.com/go-gl/gl/v3.3-core/gl"
)

func compileShader(source string, kind uint32) (uint32, error) {
	shader := gl.CreateShader(kind)
	src, free := gl.Strs(source + "\x00")
	gl.ShaderSource(shader, 1, src, nil)
	free()
	gl.CompileShader(shader)
	if err := checkStatus(shader, gl.COMPILE_STATUS, gl.GetShaderiv, gl.GetShaderInfoLog); err != nil {
		return 0, fmt.Errorf("compile shader: %w", err)
	}
	return shader, nil
}

func linkProgram(vertSrc, fragSrc string) (uint32, error) {
	vert, err := compileShader(vertSrc, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(vert)
	frag, err := compileShader(fragSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}
	defer gl.DeleteShader(frag)

	prog := gl.CreateProgram()
	gl.AttachShader(prog, vert)
	gl.AttachShader(prog, frag)
	gl.LinkProgram(prog)
	if err := checkStatus(prog, gl.LINK_STATUS, gl.GetProgramiv, gl.GetProgramInfoLog); err != nil {
		return 0, fmt.Errorf("link program: %w", err)
	}
	return prog, nil
}

func checkStatus(obj uint32, statusEnum uint32, getIV func(uint32, uint32, *int32), getLog func(uint32, int32, *int32, *uint8)) error {
	var ok int32
	getIV(obj, statusEnum, &ok)
	if ok != 0 {
		return nil
	}
	var length int32
	getIV(obj, gl.INFO_LOG_LENGTH, &length)
	log := strings.Repeat("\x00", int(length+1))
	getLog(obj, length, nil, gl.Str(log))
	return fmt.Errorf("%s", log)
}
