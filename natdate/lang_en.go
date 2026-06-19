package natdate

import "time"

var enLexicon = Lexicon{
	Code: "en",
	Weekdays: map[string]time.Weekday{
		"monday": time.Monday, "mon": time.Monday,
		"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
		"wednesday": time.Wednesday, "wed": time.Wednesday,
		"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
		"friday": time.Friday, "fri": time.Friday,
		"saturday": time.Saturday, "sat": time.Saturday,
		"sunday": time.Sunday, "sun": time.Sunday,
	},
	Units: map[string]Unit{
		"day": UnitDay, "days": UnitDay,
		"week": UnitWeek, "weeks": UnitWeek,
		"month": UnitMonth, "months": UnitMonth,
		"year": UnitYear, "years": UnitYear,
	},
	Keywords: map[string]int{
		"today":     0,
		"tomorrow":  1,
		"yesterday": -1,
	},
	Prefixes: map[string]role{
		"in":   roleIn,
		"at":   roleAt,
		"next": roleNext,
		"this": roleThis,
		"last": roleLast,
	},
	PastTails: map[string]bool{
		"ago": true,
	},
}
