package vtparse

import (
	"reflect"
	"testing"
)

// fakeSink records CSIDispatch calls, for asserting exactly what params/
// subs a sequence parses into without needing a real screen.Handler.
type fakeSink struct {
	calls [][2]any // {params []int, subs [][]int}
}

func (f *fakeSink) Print(r rune)   {}
func (f *fakeSink) Execute(b byte) {}
func (f *fakeSink) CSIDispatch(final byte, params []int, subs [][]int, intermediates []byte, private byte) {
	f.calls = append(f.calls, [2]any{append([]int(nil), params...), append([][]int(nil), subs...)})
}
func (f *fakeSink) EscDispatch(final byte, intermediates []byte) {}
func (f *fakeSink) OSCDispatch(data []byte)                      {}
func (f *fakeSink) DCSStart(final byte, params []int, subs [][]int, intermediates []byte, private byte) {
}
func (f *fakeSink) DCSPut(b byte) {}
func (f *fakeSink) DCSEnd()       {}

// TestColonSubParamParsing locks in the fix for a parser bug where a
// colon sub-parameter (e.g. curly underline's "CSI 4:3m") was silently
// dropped without resetting the accumulator, corrupting "4:3" into the
// unrelated SGR code 43 (set background red) instead of parsing as
// params=[4] with subs=[[3]].
func TestColonSubParamParsing(t *testing.T) {
	sink := &fakeSink{}
	p := New(sink)
	p.Write([]byte("\x1b[4:3m"))

	if len(sink.calls) != 1 {
		t.Fatalf("got %d CSIDispatch calls, want 1", len(sink.calls))
	}
	params := sink.calls[0][0].([]int)
	subs := sink.calls[0][1].([][]int)
	if !reflect.DeepEqual(params, []int{4}) {
		t.Fatalf("params = %v, want [4]", params)
	}
	if !reflect.DeepEqual(subs, [][]int{{3}}) {
		t.Fatalf("subs = %v, want [[3]]", subs)
	}
}

// TestColonSubParamMultipleFields exercises an underline-color-shaped
// sequence ("58:2::255:128:0") with multiple sub-parameters, some empty,
// mixed with a following plain (no-colon) param.
func TestColonSubParamMultipleFields(t *testing.T) {
	sink := &fakeSink{}
	p := New(sink)
	p.Write([]byte("\x1b[58:2::255:128:0;1m"))

	if len(sink.calls) != 1 {
		t.Fatalf("got %d CSIDispatch calls, want 1", len(sink.calls))
	}
	params := sink.calls[0][0].([]int)
	subs := sink.calls[0][1].([][]int)
	if !reflect.DeepEqual(params, []int{58, 1}) {
		t.Fatalf("params = %v, want [58 1]", params)
	}
	want := [][]int{{2, 0, 255, 128, 0}, nil}
	if !reflect.DeepEqual(subs, want) {
		t.Fatalf("subs = %v, want %v", subs, want)
	}
}

// TestPlainSemicolonParamsUnaffected is a regression check that ordinary
// semicolon-separated params (no colon anywhere) are unaffected by the
// colon-handling addition.
func TestPlainSemicolonParamsUnaffected(t *testing.T) {
	sink := &fakeSink{}
	p := New(sink)
	p.Write([]byte("\x1b[1;31m"))

	if len(sink.calls) != 1 {
		t.Fatalf("got %d CSIDispatch calls, want 1", len(sink.calls))
	}
	params := sink.calls[0][0].([]int)
	subs := sink.calls[0][1].([][]int)
	if !reflect.DeepEqual(params, []int{1, 31}) {
		t.Fatalf("params = %v, want [1 31]", params)
	}
	if !reflect.DeepEqual(subs, [][]int{nil, nil}) {
		t.Fatalf("subs = %v, want [nil nil]", subs)
	}
}
