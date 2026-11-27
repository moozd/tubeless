//go:build !cef

package cef

import "time"

func Available() bool                                            { return false }
func PrepareApplication()                                        {}
func CurrentKeyChars() (uint16, uint16, bool)                    { return 0, 0, false }
func Init(Options) error                                         { return ErrUnavailable }
func Shutdown()                                                  {}
func SetWake(func())                                             {}
func NextPump(time.Time) (time.Duration, bool)                   { return 0, false }
func Pump()                                                      {}
func SetSchemeHandler(func(SchemeRequest) SchemeResponse)        {}
func SetConsoleHandler(func(*Browser, int, string, string, int)) {}

// Browser is never created in a build without the `cef` tag.
type Browser struct{}

func NewBrowser(string, int, int, float32, uint32, int, func(*GPUFrame)) (*Browser, error) {
	return nil, ErrUnavailable
}

func (*Browser) TakeFrame(bool, func(*Frame)) bool                   { return false }
func (*Browser) Closed() bool                                        { return true }
func (*Browser) Resize(int, int, float32)                            {}
func (*Browser) Close()                                              {}
func (*Browser) Release()                                            {}
func (*Browser) Focus(bool)                                          {}
func (*Browser) MouseMove(int, int, uint32, bool)                    {}
func (*Browser) MouseClick(int, int, uint32, MouseButton, bool, int) {}
func (*Browser) MouseWheel(int, int, uint32, int, int)               {}
func (*Browser) ID() int                                             { return 0 }
func (*Browser) ExecJS(string)                                       {}
func (*Browser) Invalidate()                                         {}
func (*Browser) Key(KeyEvent)                                        {}
func (*Browser) Edit(EditCommand)                                    {}
