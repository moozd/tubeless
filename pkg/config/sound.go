package config

// Sound groups the audio effects. Sits outside both the Theme and Preset
// axes, like Font/Padding: something the user opts into directly, never
// reseeded by a theme or preset switch.
type Sound struct {
	Tumbler Tumbler `toml:"tumbler"`
}

// Tumbler is the safe-dial effect: the screen's content changes turn an
// imaginary combination-lock knob, one mechanical detent click per few
// changed cells — a trickle of typing clicks slowly, a full-screen redraw
// spins the knob hard. Off by default; see pkg/tumbler.
type Tumbler struct {
	Enabled bool `toml:"enabled"`
	// Volume is the master gain, 0-1.
	Volume float64 `toml:"volume"`
}
