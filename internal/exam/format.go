package exam

import "strconv"

// FormatPoints renders a point value without trailing zeros ("2", "1.5").
func FormatPoints(p float64) string { return strconv.FormatFloat(p, 'f', -1, 64) }
