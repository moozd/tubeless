package sixel

import "testing"

func feed(d *Decoder, s string) {
	for _, b := range []byte(s) {
		d.Put(b)
	}
}

func TestDecodeSingleColumnRed(t *testing.T) {
	d := NewDecoder()
	feed(d, "#0;2;100;0;0~") // define+select register 0 as red, full column
	img := d.Image()
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 6 {
		t.Fatalf("bad size: %v", img.Bounds())
	}
	for y := range 6 {
		c := img.RGBAAt(0, y)
		if c.R != 255 || c.G != 0 || c.B != 0 || c.A != 255 {
			t.Fatalf("pixel (0,%d) = %+v, want opaque red", y, c)
		}
	}
}

func TestDecodeRepeat(t *testing.T) {
	d := NewDecoder()
	feed(d, "#0;2;0;100;0!3~") // green, 3 full columns via repeat
	img := d.Image()
	if img.Bounds().Dx() != 3 {
		t.Fatalf("width = %d, want 3", img.Bounds().Dx())
	}
	for x := range 3 {
		if c := img.RGBAAt(x, 0); c.G != 255 {
			t.Fatalf("column %d not green: %+v", x, c)
		}
	}
}

func TestDecodeNewlineAndCarriageReturn(t *testing.T) {
	d := NewDecoder()
	// row band 0: one red column, CR, one blue column (overlapping x=0)
	// then newline, row band 1: one green column at x=0.
	feed(d, "#1;2;100;0;0~$#2;2;0;0;100~-#3;2;0;100;0~")
	img := d.Image()
	if img.Bounds().Dy() != 12 {
		t.Fatalf("height = %d, want 12 (two 6-row bands)", img.Bounds().Dy())
	}
	if c := img.RGBAAt(0, 0); c.B != 255 {
		t.Fatalf("expected blue to overwrite red at (0,0): %+v", c)
	}
	if c := img.RGBAAt(0, 6); c.G != 255 {
		t.Fatalf("expected green in second band at (0,6): %+v", c)
	}
}

func TestDecodeUndefinedRegisterDefaultsToWhite(t *testing.T) {
	d := NewDecoder()
	feed(d, "#5~") // select register 5 without ever defining it
	img := d.Image()
	c := img.RGBAAt(0, 0)
	if c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("undefined register = %+v, want white default", c)
	}
}
