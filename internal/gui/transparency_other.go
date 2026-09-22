//go:build !linux

package gui

import "unsafe"

func applyWindowTransparency(win unsafe.Pointer, opacityPercent int) {
	// No-op em plataformas não-Linux
}

func hideWindow(win unsafe.Pointer) {}
func showWindow(win unsafe.Pointer) {}
