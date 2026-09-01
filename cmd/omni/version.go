package main

import (
	"fmt"
	"runtime"
)

func versionString() string {
	return fmt.Sprintf("omni %s (%s, %s, %s %s/%s)",
		version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
