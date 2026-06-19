// Package natdate parses natural-language date/time phrases into a time.Time.
//
// It is language-agnostic: each language contributes a Lexicon (surface forms →
// meaning), and English and Polish ship built in. Lexicons of the enabled
// languages are merged, so phrases in either language parse out of the box
// (WithLanguages narrows this when a single language is wanted).
//
// Supported phrasings (English / Polish):
//
//	keywords      today / tomorrow / yesterday        — dziś, dzisiaj / jutro / wczoraj
//	weekdays      monday, mon, tue …                  — poniedziałek, pon, wt …
//	this/next     this friday, next monday            — w piątek, przyszły poniedziałek
//	last          last friday, last week              — zeszły piątek, w zeszłym tygodniu
//	relative +    in 2 weeks, 3 days from now         — za 2 tygodnie, za 5 dni
//	relative -    2 weeks ago, 3 days ago             — 2 tygodnie temu, 5 dni temu
//	periods       next week, this month, last year    — przyszły tydzień, w zeszłym roku
//	time of day   at 19:09, at 7pm                    — o 19:09
//	combinations  wednesday at 19:09                  — środa o 19:09
//	absolute      2026-06-24, 2026/06/24              — 24.06.2026, 24.06 (this year)
//
// Polish numeral-noun agreement (tydzień/tygodnie/tygodni) is handled by listing
// every inflected form in the lexicon; the parser never computes grammar.
//
// The package returns time.Time only and never formats — callers apply their own
// Go layout. The reference "now" is always passed in, so there is no hidden clock.
package natdate
