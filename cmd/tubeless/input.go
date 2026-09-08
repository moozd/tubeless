package main

import (
	"github.com/go-gl/glfw/v3.4/glfw"

	"github.com/moozd/tubeless/pkg/ptyio"
	"github.com/moozd/tubeless/pkg/render"
)

var specialKeys = map[glfw.Key][]byte{
	glfw.KeyEnter:     {'\r'},
	glfw.KeyBackspace: {0x7f},
	glfw.KeyTab:       {'\t'},
	glfw.KeyEscape:    {0x1b},
	glfw.KeyUp:        {0x1b, '[', 'A'},
	glfw.KeyDown:      {0x1b, '[', 'B'},
	glfw.KeyRight:     {0x1b, '[', 'C'},
	glfw.KeyLeft:      {0x1b, '[', 'D'},
}

func wireInput(win *render.Window, sess *ptyio.Session) {
	win.SetCharCallback(func(_ *glfw.Window, r rune) {
		sess.Write([]byte(string(r)))
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		handleSpecialKey(sess, key, mods)
	})
}

func handleSpecialKey(sess *ptyio.Session, key glfw.Key, mods glfw.ModifierKey) {
	if mods&glfw.ModControl != 0 && key >= glfw.KeyA && key <= glfw.KeyZ {
		sess.Write([]byte{byte(key-glfw.KeyA) + 1})
		return
	}
	if seq, ok := specialKeys[key]; ok {
		sess.Write(seq)
	}
}
