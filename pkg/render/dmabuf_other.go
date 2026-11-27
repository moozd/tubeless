//go:build !linux

package render

import "errors"

// GPUImportAvailable is false off Linux: there are no dmabufs.
func GPUImportAvailable() bool { return false }

func bindDMABuf(DMABuf) error {
	return errors.New("dmabuf import is Linux only")
}
