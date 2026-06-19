package natdate

import "time"

var plLexicon = Lexicon{
	Code: "pl",
	Weekdays: map[string]time.Weekday{
		// nominative + accusative (after "w"/"we") + common abbreviations.
		"poniedziałek": time.Monday, "pon": time.Monday, "pn": time.Monday,
		"wtorek": time.Tuesday, "wt": time.Tuesday,
		"środa": time.Wednesday, "środę": time.Wednesday, "śr": time.Wednesday,
		"czwartek": time.Thursday, "czw": time.Thursday,
		"piątek": time.Friday, "pt": time.Friday, "pią": time.Friday,
		"sobota": time.Saturday, "sobotę": time.Saturday, "sob": time.Saturday,
		"niedziela": time.Sunday, "niedzielę": time.Sunday, "niedz": time.Sunday, "nd": time.Sunday,
	},
	Units: map[string]Unit{
		// numeral-noun agreement: 1 dzień / 2-4 dni / 5+ dni, etc. The locative
		// forms (tygodniu/miesiącu/roku) appear in "w <adj> <noun>" period phrases.
		"dzień": UnitDay, "dni": UnitDay,
		"tydzień": UnitWeek, "tygodnie": UnitWeek, "tygodni": UnitWeek, "tygodniu": UnitWeek,
		"miesiąc": UnitMonth, "miesiące": UnitMonth, "miesięcy": UnitMonth, "miesiącu": UnitMonth,
		"rok": UnitYear, "lata": UnitYear, "lat": UnitYear, "roku": UnitYear,
	},
	Keywords: map[string]int{
		"dziś":         0,
		"dzisiaj":      0,
		"jutro":        1,
		"pojutrze":     2,
		"wczoraj":      -1,
		"przedwczoraj": -2,
	},
	Prefixes: map[string]role{
		"za": roleIn,
		"o":  roleAt,
		// "next": adjective agrees with the noun's gender/case, so list the forms.
		"przyszły": roleNext, "przyszłym": roleNext, "przyszłą": roleNext, "przyszłe": roleNext,
		// "this": "w"/"we" introduce a weekday or period; "tym" in "w tym tygodniu".
		"w": roleThis, "we": roleThis, "tym": roleThis,
		// "last": zeszły/ubiegły/ostatni and their inflections.
		"zeszły": roleLast, "zeszłym": roleLast, "zeszłą": roleLast, "zeszłe": roleLast,
		"ubiegły": roleLast, "ubiegłym": roleLast, "ubiegłą": roleLast, "ubiegłe": roleLast,
		"ostatni": roleLast, "ostatnim": roleLast, "ostatnią": roleLast, "ostatnie": roleLast,
	},
	PastTails: map[string]bool{
		"temu": true,
	},
}
