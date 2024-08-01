package etc

import (
	"math"
	"time"

	"github.com/nrednav/cuid2"
)

func NewFreshID() string {
	return cuid2.Generate()
}

func JulianDayToTime(f float64) time.Time {
	// Julian date starts at noon on January 1, 4713 BC
	const julianEpoch = 2440587.5 // Julian date for Unix epoch (January 1, 1970)

	// Convert Julian day to Unix timestamp
	unixTime := (f - julianEpoch) * 86400.0 // 86400 seconds in a day

	// Create a time.Time object from the Unix timestamp
	t := time.Unix(
		int64(unixTime),
		int64((unixTime-math.Floor(unixTime))*1e9),
	)

	return t
}

func TimeToJulianDay(t time.Time) float64 {
	// Julian date starts at noon on January 1, 4713 BC
	const julianEpoch = 2440587.5 // Julian date for Unix epoch (January 1, 1970)

	// Convert Unix timestamp to Julian day
	unixTime := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	julianDay := (unixTime / 86400.0) + julianEpoch

	return julianDay
}
