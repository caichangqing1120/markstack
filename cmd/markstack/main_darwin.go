//go:build darwin

package main

import "runtime"

func init() {
	runtime.LockOSThread()
}
