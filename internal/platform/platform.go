package platform

import "runtime"

func CurrentOS() string {
	return runtime.GOOS
}
