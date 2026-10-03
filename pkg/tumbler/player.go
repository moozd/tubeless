package tumbler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// deviceBuffer is the audio device's buffer. Small, because it is the
// delay between a screen change and its click.
const deviceBuffer = 20 * time.Millisecond

// Player owns the audio device and the Engine feeding it. The device is
// opened on the first Feed that has something to play, so a disabled
// tumbler costs nothing and never touches audio.
type Player struct {
	engine *Engine
	once   sync.Once
	ready  chan struct{}
}

// NewPlayer returns a Player with no device open yet.
func NewPlayer() *Player {
	return &Player{engine: NewEngine(), ready: make(chan struct{})}
}

// Feed turns the knob by changedCells cells' worth of change at the
// given volume (0-1). Changes fed before the device finishes opening
// are dropped — a click arriving late would be worse than none.
func (p *Player) Feed(changedCells int, volume float64) {
	if changedCells <= 0 {
		return
	}
	p.once.Do(func() { go p.open() })
	select {
	case <-p.ready:
	default:
		return
	}
	p.engine.SetVolume(volume)
	p.engine.Feed(changedCells)
}

// open starts the device. A failure is logged once and leaves the
// Player permanently silent.
func (p *Player) open() {
	err := p.start()
	if err != nil {
		log.Printf("tumbler sound disabled: %v", err)
		return
	}
	close(p.ready)
}

func (p *Player) start() error {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   deviceBuffer,
	})
	if err != nil {
		return fmt.Errorf("open audio device: %w", err)
	}
	<-ready
	player := ctx.NewPlayer(p.engine)
	player.SetBufferSize(sampleRate * 4 * int(deviceBuffer/time.Millisecond) / 1000)
	player.Play()
	return nil
}
