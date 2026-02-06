# Style and Conventions

## Code Style
- Australian English spelling in identifiers and comments (colour, normalise, etc.)
- Table-driven tests with testify/assert
- Arrange-Act-Assert test pattern
- Explicit error handling, early returns
- Small interfaces, composition over inheritance

## UI Conventions
- Colour gradients: green-to-red for usage percentages, gold-to-green for time remaining
- Progress bars: 45 chars wide with `[█░]` format
- ANSI cursor positioning for column alignment
- Lipgloss for all terminal styling

## Token Accounting (Critical)
- `DisplayTokens`: Input + output only (UI display, matches Python implementation)
- `TotalTokens`: All tokens including cache (cost calculations)
- Never confuse these - different purposes

## Session Handling
- 5-hour rolling windows
- Start times rounded DOWN to current hour
- Gap blocks for >= 5 hour gaps between activity
- Burn rate: proportional overlapping sessions in last hour
