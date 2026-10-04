Country flags, 4:3, from flag-icons 7.5.0 (MIT) — https://github.com/lipis/flag-icons

They are vendored rather than imported because Windows ships no flags in its
emoji font: a regional-indicator pair renders there as the two letters it is
built from, so the card needs actual images to show the same thing on both
platforms.

Named by lowercase ISO 3166-1 alpha-2, which is what the lookup answers with, so
the card builds the path from the code and needs no table.
