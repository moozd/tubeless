package render

import (
	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/moozd/tubeless/pkg/config"
	"github.com/moozd/tubeless/pkg/screen"
)

// Tuning constants for the GPU pixel-diff detector — the image-mode
// counterpart to shiftdetect.go's fuzzyRowMaxDiff/confidenceThreshold/
// unshiftedMaxChangedSpan. These operate on a different metric (summed
// byte distance across pixel-signature samples, not a character count),
// so the CPU path's numeric values don't carry over directly — only
// their ratio/count-based siblings (minTier1Ratio, popularityCap,
// anchorlessLineFactor) are automatically reused as-is, since they're
// baked into screen.DetectShift's own body and apply to every caller
// regardless of what a "match" is made of. imageRowConfidence/
// imageRowMinLines and their column counterparts are this file's own
// copies of the same underlying thresholds — same meaning, independent
// values, since Go can't share an unexported constant across packages.
//
// Every value here is a first guess, not a tuned result — this mode is
// explicitly experimental (see config.Scrolling.ContentShiftMode's doc)
// and expected to need adjustment once watched against real terminal
// content.
const (
	// imageSigSamples is how many signature slices SignaturePass reduces
	// each row/column to.
	imageSigSamples = 24

	// imageAuxSkipSamples mirrors shiftAuxSkip's leading-gutter tolerance
	// in signature-sample units instead of cell-column units.
	imageAuxSkipSamples = imageSigSamples / 4

	// imageFuzzyMaxDist caps tier 2's per-line tolerance: the summed
	// absolute R+G+B byte distance (0-255 per channel) across all
	// imageSigSamples samples between two rows/columns.
	imageFuzzyMaxDist = 900

	// imageSampleMatchEps is the per-sample summed R+G+B distance below
	// which signatureAffixGap treats two samples as "the same" — the
	// pixel-domain equivalent of a character comparison.
	imageSampleMatchEps = 36

	// imageMaxChangedSpan caps the affix-gap self-match check, in
	// signature samples — see shiftdetect.go's unshiftedMaxChangedSpan
	// for the concept this mirrors.
	imageMaxChangedSpan = imageSigSamples / 2

	imageRowConfidence = 0.80
	imageRowMinLines   = 2

	imageColConfidence = 0.94
	imageColMinLines   = 20
)

// DetectImageRowShift is DetectContentShift's GPU pixel-diff sibling for
// vertical shifts — see config.Scrolling.ContentShiftMode's "image"
// value. It renders scr into a small off-pipeline scratch buffer at a
// forced zero shift offset (never the in-progress glide's own offset:
// the glide's starting position must be decided from the TRUE, unshifted
// content, exactly like the CPU path diffs scr.Grid directly rather than
// anything already-animated), reduces that to a compact per-row pixel
// signature, and diffs it against the previous call's signature using
// the exact same screen.DetectShift engine the CPU content-diff path
// uses — only the per-line hash/fuzzy/affix signal differs. Unlike
// DetectContentShift, this doesn't fall back to columnBands' split-pane
// search — a deliberate v1 scope cut, matching the "start without extra
// machinery and see how robust the plain diff is" brief this mode
// shipped under.
func (r *Renderer) DetectImageRowShift(scr *screen.Screen, cfg config.Config, cellW, cellH float32, maxShift int) (screen.RowShift, bool) {
	cols, rows := scr.Cols, scr.Rows
	r.ensureImageContent(scr, cfg, cellW, cellH)
	r.signaturePass.DrawRowSignature(r.imageContentFBO.tex, r.imageRowSigFBO, imageSigSamples, rows)
	sig := readFBOBytes(r.imageRowSigFBO)

	prev, prevCols, prevRows := r.imagePrevRowSig, r.imagePrevRowCols, r.imagePrevRowRows
	r.imagePrevRowSig, r.imagePrevRowCols, r.imagePrevRowRows = sig, cols, rows
	if prev == nil || prevCols != cols || prevRows != rows {
		return screen.RowShift{}, false
	}

	blankLine := r.imageBlankSignature(cfg, cellW, cellH)
	blank := hashSignature(blankLine)
	auxBlank := hashSignature(blankLine[imageAuxSkipSamples*4:])

	beforeHash, afterHash, auxBeforeHash, auxAfterHash := signatureHashes(prev, sig, rows)
	fuzzy := func(afterIdx, beforeIdx int) int {
		return signatureDist(lineBytes(sig, afterIdx), lineBytes(prev, beforeIdx))
	}
	affixGap := func(afterIdx, beforeIdx int) int {
		return signatureAffixGap(lineBytes(sig, afterIdx), lineBytes(prev, beforeIdx))
	}
	lo, hi, delta, ok := screen.DetectShift(rows, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash,
		fuzzy, affixGap, imageFuzzyMaxDist, imageMaxChangedSpan, imageRowConfidence, imageRowMinLines, blank, auxBlank)
	if !ok {
		return screen.RowShift{}, false
	}
	return screen.RowShift{Top: lo, Bottom: hi, Left: 0, Right: cols - 1, Delta: delta}, true
}

// DetectImageColShift is DetectImageRowShift's horizontal mirror, fed by
// the same GPU signature pipeline transposed onto the column axis.
func (r *Renderer) DetectImageColShift(scr *screen.Screen, cfg config.Config, cellW, cellH float32, maxShift int) (screen.ColShift, bool) {
	cols, rows := scr.Cols, scr.Rows
	r.ensureImageContent(scr, cfg, cellW, cellH)
	r.signaturePass.DrawColSignature(r.imageContentFBO.tex, r.imageColSigFBO, cols, imageSigSamples)
	sig := transposeColumns(readFBOBytes(r.imageColSigFBO), cols, imageSigSamples)

	prev, prevCols, prevRows := r.imagePrevColSig, r.imagePrevColCols, r.imagePrevColRows
	r.imagePrevColSig, r.imagePrevColCols, r.imagePrevColRows = sig, cols, rows
	if prev == nil || prevCols != cols || prevRows != rows {
		return screen.ColShift{}, false
	}

	blankLine := r.imageBlankSignature(cfg, cellW, cellH)
	blank := hashSignature(blankLine)
	auxBlank := hashSignature(blankLine[imageAuxSkipSamples*4:])

	beforeHash, afterHash, auxBeforeHash, auxAfterHash := signatureHashes(prev, sig, cols)
	fuzzy := func(afterIdx, beforeIdx int) int {
		return signatureDist(lineBytes(sig, afterIdx), lineBytes(prev, beforeIdx))
	}
	affixGap := func(afterIdx, beforeIdx int) int {
		return signatureAffixGap(lineBytes(sig, afterIdx), lineBytes(prev, beforeIdx))
	}
	lo, hi, delta, ok := screen.DetectShift(cols, maxShift, beforeHash, afterHash, auxBeforeHash, auxAfterHash,
		fuzzy, affixGap, imageFuzzyMaxDist, imageMaxChangedSpan, imageColConfidence, imageColMinLines, blank, auxBlank)
	if !ok {
		return screen.ColShift{}, false
	}
	return screen.ColShift{Left: lo, Right: hi, Top: 0, Bottom: rows - 1, Delta: delta}, true
}

// ensureImageContent renders scr's literal content (background fills +
// text glyphs only — line art/underlines/images are a v1 scope cut, easy
// to add if signatures prove too weak without them) into imageContentFBO
// at a forced zero shift offset, unless it already holds exactly this —
// memoized by scr's pointer identity (a published Screen is an immutable
// snapshot, same convention cmd/tubeless already relies on for scr !=
// lastScr) plus cellW/cellH, so DetectImageRowShift and
// DetectImageColShift called back to back for the same frame only draw
// it once.
func (r *Renderer) ensureImageContent(scr *screen.Screen, cfg config.Config, cellW, cellH float32) {
	if r.imageContentScr == scr && r.imageContentCellW == cellW && r.imageContentCellH == cellH {
		return
	}
	r.renderZeroOffsetContent(scr, cfg, cellW, cellH, r.imageContentFBO)
	r.imageContentScr, r.imageContentCellW, r.imageContentCellH = scr, cellW, cellH
}

// renderZeroOffsetContent draws scr into dst exactly like RenderScene's
// literal-image passes, minus the cosmetic surface/shadow/blur/bloom
// compositing (irrelevant to a pixel signature meant to track real
// content, not present-frame styling) — always at rowShift/colShift's
// zero value regardless of any glide currently in progress on the
// Renderer, and with no selection highlight, so the signature reflects
// pure content the same way scr.Grid does for the CPU content-diff path.
// Overwrites CellPass's shared instance scratch buffers, but harmlessly:
// callers run this before the frame's real PrepareFrame (which rebuilds
// those buffers with the real shift/selection before RenderScene ever
// draws them to sceneFBO).
func (r *Renderer) renderZeroOffsetContent(scr *screen.Screen, cfg config.Config, cellW, cellH float32, dst *FBO) {
	r.cellPass.BuildInstances(scr, cfg, cellW, cellH, 0, Selection{}, RowShift{}, ColShift{})
	gridW := int(cellW*float32(scr.Cols) + 0.5)
	gridH := int(cellH*float32(scr.Rows) + 0.5)
	dst.Resize(gridW, gridH)
	dst.ClearOpaque()
	if cfg.TrueColor {
		r.cellPass.DrawAmbientBG(dst, gridW, gridH, cfg.Colors.DefaultBg)
	}
	r.cellPass.DrawRects(dst, cellW, cellH)
	r.cellPass.DrawText(dst, cellW, cellH)
}

// imageBlankSignature renders a single blank cell through the same
// pipeline as renderZeroOffsetContent/DrawRowSignature and returns its
// signature bytes — the reference an all-background row or column's real
// rendered signature should hash-match exactly, used as DetectShift's
// blank/auxBlank sentinels. Computed from the actual pipeline (not
// predicted analytically from cfg.Colors.DefaultBg) since the real bytes
// depend on sRGB encoding/blend details that are easy to get subtly
// wrong by hand — recomputed on every call rather than cached: it's a
// single-cell draw plus a tiny readback, cheap enough next to the
// full-grid signature this same call already computes.
func (r *Renderer) imageBlankSignature(cfg config.Config, cellW, cellH float32) []byte {
	if r.imageBlankScr == nil {
		r.imageBlankScr = screen.New(1, 1)
	}
	r.renderZeroOffsetContent(r.imageBlankScr, cfg, cellW, cellH, r.imageBlankFBO)
	r.signaturePass.DrawRowSignature(r.imageBlankFBO.tex, r.imageBlankSigFBO, imageSigSamples, 1)
	return readFBOBytes(r.imageBlankSigFBO)
}

// readFBOBytes reads an FBO's full RGBA8 content back to the CPU. Used
// only for the small signature/blank-reference targets (a few KB at
// most) this file computes — never the main scene, which stays entirely
// on the GPU.
func readFBOBytes(fbo *FBO) []byte {
	buf := make([]byte, fbo.W*fbo.H*4)
	fbo.Bind()
	gl.ReadPixels(0, 0, int32(fbo.W), int32(fbo.H), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(buf))
	fbo.Unbind()
	return buf
}

// transposeColumns rearranges a cols x sigLen RGBA signature (row-major,
// as ReadPixels returns a DrawColSignature target) into sigLen-samples-
// per-column contiguous blocks, one per column — the column axis's
// mirror of the row axis's already-contiguous-per-row layout, so both
// axes can share lineBytes/hashSignature/signatureDist/signatureAffixGap
// below without a second copy of each.
func transposeColumns(sig []byte, cols, sigLen int) []byte {
	out := make([]byte, cols*sigLen*4)
	for s := range sigLen {
		for x := range cols {
			srcOff := (s*cols + x) * 4
			dstOff := (x*sigLen + s) * 4
			copy(out[dstOff:dstOff+4], sig[srcOff:srcOff+4])
		}
	}
	return out
}

// lineBytes returns line i's imageSigSamples*4 raw RGBA bytes out of buf
// (rows for a row signature, columns for a column signature — both laid
// out contiguously per line, the row axis natively and the column axis
// via transposeColumns).
func lineBytes(buf []byte, i int) []byte {
	return buf[i*imageSigSamples*4 : (i+1)*imageSigSamples*4]
}

// signatureHashes builds DetectShift's four hash arrays from two
// contiguous-per-line signature buffers — the image-diff mirror of
// detectContentShiftInBand's rowHash/auxBeforeHash loop, just hashing
// pixel-signature bytes instead of Cell.Rune content.
func signatureHashes(before, after []byte, n int) (beforeHash, afterHash, auxBeforeHash, auxAfterHash []uint64) {
	beforeHash = make([]uint64, n)
	afterHash = make([]uint64, n)
	auxBeforeHash = make([]uint64, n)
	auxAfterHash = make([]uint64, n)
	for i := range n {
		beforeHash[i] = hashSignature(lineBytes(before, i))
		afterHash[i] = hashSignature(lineBytes(after, i))
		auxBeforeHash[i] = hashSignature(lineBytes(before, i)[imageAuxSkipSamples*4:])
		auxAfterHash[i] = hashSignature(lineBytes(after, i)[imageAuxSkipSamples*4:])
	}
	return
}

// hashSignature is an FNV-1a hash over raw signature bytes — the
// pixel-domain sibling of shiftdetect.go's foldRune, folding a byte at a
// time instead of a 4-byte rune.
func hashSignature(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, v := range b {
		h = (h ^ uint64(v)) * 1099511628211
	}
	return h
}

// signatureDist is tier 2's per-line check: the summed absolute R+G+B
// byte distance between two lines' signatures — the pixel-domain sibling
// of shiftdetect.go's rowMismatches, an absolute count rather than a
// fraction for the same reason (see fuzzyRowMaxDiff's doc).
func signatureDist(a, b []byte) int {
	dist := 0
	for i := 0; i+4 <= len(a); i += 4 {
		dist += absInt(int(a[i]) - int(b[i]))
		dist += absInt(int(a[i+1]) - int(b[i+1]))
		dist += absInt(int(a[i+2]) - int(b[i+2]))
	}
	return dist
}

// signatureAffixGap is unshiftedMatch's structural check in pixel-space:
// the number of samples between a's and b's longest common (within
// imageSampleMatchEps) leading and trailing run — the sibling of
// shiftdetect.go's cellAffixGap, at sample granularity instead of
// per-character.
func signatureAffixGap(a, b []byte) int {
	samples := len(a) / 4
	sameAt := func(i int) bool {
		off := i * 4
		d := absInt(int(a[off])-int(b[off])) + absInt(int(a[off+1])-int(b[off+1])) + absInt(int(a[off+2])-int(b[off+2]))
		return d <= imageSampleMatchEps
	}
	prefix := 0
	for prefix < samples && sameAt(prefix) {
		prefix++
	}
	suffix := 0
	for suffix < samples-prefix && sameAt(samples-1-suffix) {
		suffix++
	}
	return samples - prefix - suffix
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
