# Task Completion Checklist

Before declaring a task complete, verify:

1. `go build ./...` - Code compiles without errors
2. `make test` - All tests pass (race detection enabled)
3. `make lint` - No new lint warnings
4. No debug statements remain (fmt.Println, log.Printf debug, etc.)
5. Error handling is in place for new code paths
6. Australian English spelling in identifiers and comments
