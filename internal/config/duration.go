package config

import (
	"regexp"
	"strconv"
	"time"
)

// Duration is a time.Duration that also accepts a "d" (day) suffix in its
// text form, e.g. "14d" or "1d12h". time.ParseDuration alone does not
// understand days, but omni.conf's retention setting is naturally
// expressed in them ("14d").
type Duration time.Duration

// dayPattern matches a decimal number immediately followed by "d", so it can
// be rewritten into hours before handing the string to time.ParseDuration.
var dayPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)d`)

// ParseDuration parses s as a Go duration string, additionally accepting a
// "d" unit for days (expanded to 24h before parsing). Both "14d" and
// "1d12h30m" are valid.
func ParseDuration(s string) (time.Duration, error) {
	expanded := dayPattern.ReplaceAllStringFunc(s, func(m string) string {
		numPart := m[:len(m)-1]
		f, err := strconv.ParseFloat(numPart, 64)
		if err != nil {
			// Leave unrecognized; time.ParseDuration will report the error.
			return m
		}
		return strconv.FormatFloat(f*24, 'f', -1, 64) + "h"
	})
	return time.ParseDuration(expanded)
}

// String renders d using time.Duration's formatting.
func (d Duration) String() string { return time.Duration(d).String() }
