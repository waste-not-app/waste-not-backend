package domain

const (
	MinWeightG           = 1
	MaxWeightG           = 100000
	MinItemCount         = 1
	MaxItemCount         = 10000
	MinNameLength        = 2
	MaxNameLength        = 60
	MaxEmailLength       = 254
	MinPasswordLength    = 12
	MaxPasswordLength    = 128
	MaxNotesLength       = 500
	ActivityDatePastDays = 30
	KgMaxDecimalPlaces   = 3
)

var (
	Categories = []string{"ORGANIC", "PLASTIC", "PAPER", "GLASS", "METAL", "ELECTRONIC", "OTHER"}
	Methods    = []string{"REDUCE", "REUSE", "RECYCLE", "TRASH"}
)

func Contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
